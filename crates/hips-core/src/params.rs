use std::collections::HashMap;

use crate::color::Rgba;
use crate::fit::{self, FitMode, Gravity, Plan};
use crate::format::OutputFormat;

pub const DEFAULT_QUALITY: u8 = 85;
pub const MAX_DIMENSION: u32 = 5000;
pub const MAX_BLUR: f64 = 100.0;
pub const MAX_DPR: u32 = 3;

#[derive(Debug, Clone, PartialEq)]
pub struct ImageParams {
    pub width: Option<u32>,
    pub height: Option<u32>,
    pub quality: u8,
    pub format: OutputFormat,
    pub fit: FitMode,
    pub gravity: Gravity,
    pub dpr: u32,
    pub background: Option<Rgba>,
    pub blur: f64,
    pub brightness: Option<f64>,
    pub contrast: Option<f64>,
    pub gamma: Option<f64>,
    pub sharpen: Option<f64>,
    pub saturation: Option<f64>,
    pub rotate: u16,
    pub trim: bool,
}

impl Default for ImageParams {
    fn default() -> Self {
        ImageParams {
            width: None,
            height: None,
            quality: DEFAULT_QUALITY,
            format: OutputFormat::Keep,
            fit: FitMode::ScaleDown,
            gravity: Gravity::Center,
            dpr: 1,
            background: None,
            blur: 0.0,
            brightness: None,
            contrast: None,
            gamma: None,
            sharpen: None,
            saturation: None,
            rotate: 0,
            trim: false,
        }
    }
}

impl ImageParams {
    pub fn from_pairs<I, K, V>(pairs: I) -> ImageParams
    where
        I: IntoIterator<Item = (K, V)>,
        K: AsRef<str>,
        V: AsRef<str>,
    {
        let mut m: HashMap<String, String> = HashMap::new();
        for (k, v) in pairs {
            m.insert(
                k.as_ref().to_ascii_lowercase(),
                v.as_ref().trim().to_string(),
            );
        }
        let raw = |keys: &[&str]| -> Option<&str> {
            keys.iter()
                .find_map(|k| m.get(*k).map(String::as_str))
                .filter(|s| !s.is_empty())
        };

        let dpr = raw(&["dpr"])
            .and_then(parse_u32)
            .map(|d| d.clamp(1, MAX_DPR))
            .unwrap_or(1);

        let width = raw(&["w", "width"])
            .and_then(parse_dim)
            .map(|d| scale_dim(d, dpr));
        let height = raw(&["h", "height"])
            .and_then(parse_dim)
            .map(|d| scale_dim(d, dpr));

        let quality = raw(&["q", "quality"])
            .and_then(parse_u32)
            .map(|q| q.clamp(1, 100) as u8)
            .unwrap_or(DEFAULT_QUALITY);

        let format = raw(&["f", "format"])
            .map(OutputFormat::parse)
            .unwrap_or(OutputFormat::Keep);

        let pad_flag = raw(&["pad"]).map(parse_bool).unwrap_or(false);
        let fit = raw(&["fit"])
            .and_then(FitMode::parse)
            .unwrap_or(if pad_flag {
                FitMode::Pad
            } else {
                FitMode::ScaleDown
            });

        let gravity = raw(&["gravity", "g"])
            .and_then(Gravity::parse)
            .unwrap_or(Gravity::Center);

        let background = raw(&["background", "bg"]).and_then(Rgba::parse);

        let blur = raw(&["blur"])
            .and_then(parse_f64)
            .map(|b| b.clamp(0.0, MAX_BLUR))
            .unwrap_or(0.0);

        let rotate = raw(&["rotate"])
            .and_then(parse_u32)
            .map(|r| match r {
                90 | 180 | 270 => r as u16,
                _ => 0,
            })
            .unwrap_or(0);

        ImageParams {
            width,
            height,
            quality,
            format,
            fit,
            gravity,
            dpr,
            background,
            blur,
            brightness: raw(&["brightness"]).and_then(parse_f64),
            contrast: raw(&["contrast"]).and_then(parse_f64),
            gamma: raw(&["gamma"]).and_then(parse_f64),
            sharpen: raw(&["sharpen"]).and_then(parse_f64),
            saturation: raw(&["saturation"]).and_then(parse_f64),
            rotate,
            trim: raw(&["trim"]).map(parse_bool).unwrap_or(false),
        }
    }

    pub fn plan(&self, src_w: u32, src_h: u32) -> Plan {
        fit::plan(
            src_w,
            src_h,
            self.width,
            self.height,
            self.fit,
            self.gravity,
        )
    }

    pub fn has_color_ops(&self) -> bool {
        self.blur > 0.0
            || self.brightness.is_some()
            || self.contrast.is_some()
            || self.gamma.is_some()
            || self.sharpen.is_some()
            || self.saturation.is_some()
    }
}

fn parse_u32(s: &str) -> Option<u32> {
    s.parse::<u32>().ok()
}

fn parse_dim(s: &str) -> Option<u32> {
    match s.parse::<u32>() {
        Ok(0) => None,
        Ok(v) => Some(v),
        Err(_) => None,
    }
}

fn scale_dim(value: u32, dpr: u32) -> u32 {
    value.saturating_mul(dpr).min(MAX_DIMENSION)
}

fn parse_f64(s: &str) -> Option<f64> {
    s.parse::<f64>().ok().filter(|v| v.is_finite())
}

fn parse_bool(s: &str) -> bool {
    matches!(s.to_ascii_lowercase().as_str(), "true" | "1" | "yes")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(query: &[(&str, &str)]) -> ImageParams {
        ImageParams::from_pairs(query.iter().copied())
    }

    #[test]
    fn avatar_preset_cover_is_honored() {
        let p = parse(&[
            ("w", "200"),
            ("h", "200"),
            ("q", "90"),
            ("f", "webp"),
            ("fit", "cover"),
        ]);
        assert_eq!(p.width, Some(200));
        assert_eq!(p.height, Some(200));
        assert_eq!(p.quality, 90);
        assert_eq!(p.format, OutputFormat::Webp);
        assert_eq!(p.fit, FitMode::Cover);
    }

    #[test]
    fn defaults_match_contract() {
        let p = parse(&[]);
        assert_eq!(p.quality, DEFAULT_QUALITY);
        assert_eq!(p.format, OutputFormat::Keep);
        assert_eq!(p.fit, FitMode::ScaleDown);
        assert_eq!(p.gravity, Gravity::Center);
        assert_eq!(p.dpr, 1);
        assert!(p.width.is_none() && p.height.is_none());
    }

    #[test]
    fn dpr_scales_dimensions_and_clamps() {
        let p = parse(&[("w", "600"), ("h", "400"), ("dpr", "2")]);
        assert_eq!(p.width, Some(1200));
        assert_eq!(p.height, Some(800));

        let clamped = parse(&[("w", "3000"), ("dpr", "3")]);
        assert_eq!(clamped.width, Some(MAX_DIMENSION));
    }

    #[test]
    fn invalid_values_fall_back_to_defaults() {
        let p = parse(&[
            ("w", "abc"),
            ("q", "9999"),
            ("f", "bmp"),
            ("blur", "-5"),
            ("rotate", "45"),
        ]);
        assert_eq!(p.width, None);
        assert_eq!(p.quality, 100);
        assert_eq!(p.format, OutputFormat::Keep);
        assert_eq!(p.blur, 0.0);
        assert_eq!(p.rotate, 0);
    }

    #[test]
    fn pad_flag_selects_pad_mode_when_fit_absent() {
        let p = parse(&[("w", "400"), ("h", "400"), ("pad", "true")]);
        assert_eq!(p.fit, FitMode::Pad);
    }

    #[test]
    fn explicit_fit_overrides_pad_flag() {
        let p = parse(&[
            ("w", "400"),
            ("h", "400"),
            ("fit", "cover"),
            ("pad", "true"),
        ]);
        assert_eq!(p.fit, FitMode::Cover);
    }

    #[test]
    fn background_and_format_auto() {
        let p = parse(&[
            ("background", "#ffffff"),
            ("f", "auto"),
            ("gravity", "top-left"),
        ]);
        assert_eq!(p.background, Some(Rgba::WHITE));
        assert_eq!(p.format, OutputFormat::Auto);
        assert_eq!(p.gravity, Gravity::NorthWest);
    }
}
