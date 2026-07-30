#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Codec {
    Jpeg,
    Png,
    Webp,
    Avif,
    Gif,
}

impl Codec {
    pub fn content_type(self) -> &'static str {
        match self {
            Codec::Jpeg => "image/jpeg",
            Codec::Png => "image/png",
            Codec::Webp => "image/webp",
            Codec::Avif => "image/avif",
            Codec::Gif => "image/gif",
        }
    }

    pub fn supports_alpha(self) -> bool {
        matches!(self, Codec::Png | Codec::Webp | Codec::Avif | Codec::Gif)
    }

    pub fn from_magic(bytes: &[u8]) -> Option<Codec> {
        if bytes.len() >= 3 && bytes[0] == 0xFF && bytes[1] == 0xD8 && bytes[2] == 0xFF {
            return Some(Codec::Jpeg);
        }
        if bytes.len() >= 8 && bytes[..8] == [0x89, b'P', b'N', b'G', 0x0D, 0x0A, 0x1A, 0x0A] {
            return Some(Codec::Png);
        }
        if bytes.len() >= 6 && (&bytes[..6] == b"GIF87a" || &bytes[..6] == b"GIF89a") {
            return Some(Codec::Gif);
        }
        if bytes.len() >= 12 && &bytes[..4] == b"RIFF" && &bytes[8..12] == b"WEBP" {
            return Some(Codec::Webp);
        }
        if bytes.len() >= 12 && &bytes[4..8] == b"ftyp" {
            let brand = &bytes[8..12];
            if brand == b"avif" || brand == b"avis" {
                return Some(Codec::Avif);
            }
        }
        None
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OutputFormat {
    Keep,
    Auto,
    Jpeg,
    Png,
    Webp,
    Avif,
}

impl OutputFormat {
    pub fn parse(value: &str) -> OutputFormat {
        match value.trim().to_ascii_lowercase().as_str() {
            "auto" => OutputFormat::Auto,
            "jpeg" | "jpg" => OutputFormat::Jpeg,
            "png" => OutputFormat::Png,
            "webp" => OutputFormat::Webp,
            "avif" => OutputFormat::Avif,
            _ => OutputFormat::Keep,
        }
    }

    pub fn negotiated(self) -> bool {
        matches!(self, OutputFormat::Auto)
    }

    pub fn resolve(self, source: Option<Codec>, accept: &Accept) -> Codec {
        match self {
            OutputFormat::Jpeg => Codec::Jpeg,
            OutputFormat::Png => Codec::Png,
            OutputFormat::Webp => Codec::Webp,
            OutputFormat::Avif => Codec::Avif,
            OutputFormat::Auto => {
                if accept.avif {
                    Codec::Avif
                } else if accept.webp {
                    Codec::Webp
                } else {
                    encodable(source)
                }
            }
            OutputFormat::Keep => encodable(source),
        }
    }
}

fn encodable(source: Option<Codec>) -> Codec {
    match source {
        Some(Codec::Png) => Codec::Png,
        Some(Codec::Webp) => Codec::Webp,
        Some(Codec::Avif) => Codec::Avif,
        Some(Codec::Jpeg) => Codec::Jpeg,
        Some(Codec::Gif) => Codec::Png,
        None => Codec::Jpeg,
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Accept {
    pub avif: bool,
    pub webp: bool,
}

impl Accept {
    pub fn parse(header: Option<&str>) -> Accept {
        let header = header.unwrap_or("");
        Accept {
            avif: header.contains("image/avif"),
            webp: header.contains("image/webp"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_source_codecs() {
        assert_eq!(
            Codec::from_magic(&[0xFF, 0xD8, 0xFF, 0xE0]),
            Some(Codec::Jpeg)
        );
        assert_eq!(
            Codec::from_magic(&[0x89, b'P', b'N', b'G', 0x0D, 0x0A, 0x1A, 0x0A]),
            Some(Codec::Png)
        );
        assert_eq!(
            Codec::from_magic(b"RIFF\0\0\0\0WEBPVP8 "),
            Some(Codec::Webp)
        );
        assert_eq!(
            Codec::from_magic(&[0, 0, 0, 0x20, b'f', b't', b'y', b'p', b'a', b'v', b'i', b'f']),
            Some(Codec::Avif)
        );
    }

    #[test]
    fn auto_prefers_avif_then_webp() {
        let accept = Accept::parse(Some("image/avif,image/webp,*/*"));
        assert_eq!(
            OutputFormat::Auto.resolve(Some(Codec::Jpeg), &accept),
            Codec::Avif
        );

        let accept = Accept::parse(Some("image/webp,*/*"));
        assert_eq!(
            OutputFormat::Auto.resolve(Some(Codec::Jpeg), &accept),
            Codec::Webp
        );

        let accept = Accept::parse(Some("*/*"));
        assert_eq!(
            OutputFormat::Auto.resolve(Some(Codec::Png), &accept),
            Codec::Png
        );
    }

    #[test]
    fn keep_falls_back_for_gif() {
        let accept = Accept::default();
        assert_eq!(
            OutputFormat::Keep.resolve(Some(Codec::Gif), &accept),
            Codec::Png
        );
        assert_eq!(OutputFormat::Keep.resolve(None, &accept), Codec::Jpeg);
    }
}
