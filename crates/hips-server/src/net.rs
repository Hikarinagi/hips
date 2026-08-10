use std::net::IpAddr;

use bytes::Bytes;
use futures_util::StreamExt;
use object_store::ObjectStoreExt;
use url::Url;

use crate::config::Provider;
use crate::error::AppError;
use crate::state::AppState;

#[derive(Debug)]
pub enum Source {
    R2(String),
    Remote { url: String },
}

impl Source {
    pub fn identity(&self) -> String {
        match self {
            Source::R2(key) => format!("r2:{key}"),
            Source::Remote { url } => format!("tp:{url}"),
        }
    }
}

pub fn classify(path: &str, providers: &[Provider]) -> Result<Source, AppError> {
    let trimmed = path.trim_start_matches('/');
    if trimmed.is_empty() {
        return Err(AppError::BadRequest("empty path".to_string()));
    }

    if let Some((head, rest)) = trimmed.split_once('/') {
        if let Some(provider) = providers.iter().find(|p| p.name == head) {
            let candidate = restore_scheme(rest);
            let raw = if is_http(&candidate) {
                candidate
            } else {
                restore_scheme(&percent_encoding::percent_decode_str(rest).decode_utf8_lossy())
            };
            if is_http(&raw) {
                let url = Url::parse(&raw)
                    .map_err(|_| AppError::BadRequest("invalid remote url".to_string()))?;
                let host = url
                    .host_str()
                    .ok_or_else(|| AppError::BadRequest("invalid remote url".to_string()))?;
                if !provider
                    .allowed_hosts
                    .iter()
                    .any(|h| h.eq_ignore_ascii_case(host))
                {
                    return Err(AppError::Forbidden(format!("host not allowed: {host}")));
                }
                return Ok(Source::Remote { url: raw });
            }
        }
    }

    Ok(Source::R2(trimmed.to_string()))
}

fn is_http(value: &str) -> bool {
    let lower = value.to_ascii_lowercase();
    lower.starts_with("http://") || lower.starts_with("https://")
}

fn restore_scheme(value: &str) -> String {
    for prefix in ["https:/", "http:/"] {
        if let Some(rest) = value.strip_prefix(prefix) {
            if !rest.starts_with('/') {
                return format!("{prefix}/{rest}");
            }
        }
    }
    value.to_string()
}

pub async fn fetch_source(state: &AppState, source: &Source) -> Result<Bytes, AppError> {
    let identity = source.identity();
    match source {
        Source::R2(key) => {
            state
                .source_cache
                .get(&identity, || fetch_r2(state, key))
                .await
        }
        Source::Remote { url } => {
            state
                .source_cache
                .get(&identity, || fetch_remote(state, url))
                .await
        }
    }
}

pub async fn fetch_r2(state: &AppState, key: &str) -> Result<Bytes, AppError> {
    let path = object_store::path::Path::from(key);
    let result = state.storage.get(&path).await?;
    Ok(result.bytes().await?)
}

pub async fn fetch_remote(state: &AppState, raw_url: &str) -> Result<Bytes, AppError> {
    let url =
        Url::parse(raw_url).map_err(|_| AppError::BadRequest("invalid remote url".to_string()))?;
    if !matches!(url.scheme(), "http" | "https") {
        return Err(AppError::BadRequest("invalid remote url".to_string()));
    }

    if !state.config.allow_private_remote {
        let host = url
            .host_str()
            .ok_or_else(|| AppError::BadRequest("invalid remote url".to_string()))?;
        let port = url.port_or_known_default().unwrap_or(443);
        let mut resolved = false;
        for addr in tokio::net::lookup_host((host, port))
            .await
            .map_err(|_| AppError::Upstream)?
        {
            resolved = true;
            if is_blocked(addr.ip()) {
                return Err(AppError::Forbidden(
                    "remote address not allowed".to_string(),
                ));
            }
        }
        if !resolved {
            return Err(AppError::Upstream);
        }
    }

    let response = state
        .http
        .get(url)
        .timeout(state.config.download_timeout)
        .send()
        .await
        .map_err(|_| AppError::Upstream)?;
    if !response.status().is_success() {
        return Err(AppError::Upstream);
    }

    let limit = state.config.max_src_bytes;
    let mut buffer = Vec::new();
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|_| AppError::Upstream)?;
        if buffer.len() + chunk.len() > limit {
            return Err(AppError::TooLarge);
        }
        buffer.extend_from_slice(&chunk);
    }
    Ok(Bytes::from(buffer))
}

fn is_blocked(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            v4.is_loopback()
                || v4.is_private()
                || v4.is_link_local()
                || v4.is_broadcast()
                || v4.is_documentation()
                || v4.is_unspecified()
                || v4.is_multicast()
                || v4.octets()[0] == 0
                || (v4.octets()[0] == 100 && (v4.octets()[1] & 0xC0) == 64)
        }
        IpAddr::V6(v6) => {
            v6.is_loopback()
                || v6.is_unspecified()
                || v6.is_multicast()
                || (v6.segments()[0] & 0xffc0) == 0xfe80
                || (v6.segments()[0] & 0xfe00) == 0xfc00
                || v6
                    .to_ipv4_mapped()
                    .map(|m| is_blocked(IpAddr::V4(m)))
                    .unwrap_or(false)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn providers() -> Vec<Provider> {
        vec![Provider {
            name: "bangumi".to_string(),
            allowed_hosts: vec!["lain.bgm.tv".to_string()],
        }]
    }

    #[test]
    fn tolerates_proxy_collapsed_double_slash() {
        let p = providers();
        for path in [
            "bangumi/https://lain.bgm.tv/pic/cover/l/x.jpg",
            "bangumi/https:/lain.bgm.tv/pic/cover/l/x.jpg",
        ] {
            match classify(path, &p) {
                Ok(Source::Remote { url }) => {
                    assert_eq!(url, "https://lain.bgm.tv/pic/cover/l/x.jpg")
                }
                other => panic!("expected Remote, got {other:?}"),
            }
        }
    }

    #[test]
    fn non_provider_prefix_is_r2_key() {
        match classify("bangumi/123/cover.jpg", &providers()) {
            Ok(Source::R2(key)) => assert_eq!(key, "bangumi/123/cover.jpg"),
            other => panic!("expected R2, got {other:?}"),
        }
    }
}
