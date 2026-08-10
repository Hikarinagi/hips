#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FitMode {
    ScaleDown,
    Contain,
    Cover,
    Crop,
    Pad,
}

impl FitMode {
    pub fn parse(value: &str) -> Option<FitMode> {
        match value.trim().to_ascii_lowercase().as_str() {
            "scale-down" | "scaledown" => Some(FitMode::ScaleDown),
            "contain" => Some(FitMode::Contain),
            "cover" => Some(FitMode::Cover),
            "crop" => Some(FitMode::Crop),
            "pad" => Some(FitMode::Pad),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Gravity {
    Auto,
    Center,
    North,
    South,
    East,
    West,
    NorthEast,
    NorthWest,
    SouthEast,
    SouthWest,
    Face,
}

impl Gravity {
    pub fn parse(value: &str) -> Option<Gravity> {
        match value.trim().to_ascii_lowercase().as_str() {
            "auto" => Some(Gravity::Auto),
            "center" | "centre" => Some(Gravity::Center),
            "top" | "north" => Some(Gravity::North),
            "bottom" | "south" => Some(Gravity::South),
            "right" | "east" => Some(Gravity::East),
            "left" | "west" => Some(Gravity::West),
            "top-right" | "northeast" => Some(Gravity::NorthEast),
            "top-left" | "northwest" => Some(Gravity::NorthWest),
            "bottom-right" | "southeast" => Some(Gravity::SouthEast),
            "bottom-left" | "southwest" => Some(Gravity::SouthWest),
            "face" => Some(Gravity::Face),
            _ => None,
        }
    }

    fn smart(self) -> Option<Interest> {
        match self {
            Gravity::Center => Some(Interest::Centre),
            Gravity::Auto => Some(Interest::Attention),
            _ => None,
        }
    }
}

const FACE_VERTICAL_BIAS: f64 = 0.38;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Focus {
    pub x: f64,
    pub y: f64,
}

impl Focus {
    pub fn new(x: f64, y: f64) -> Focus {
        Focus {
            x: x.clamp(0.0, 1.0),
            y: y.clamp(0.0, 1.0),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Interest {
    Centre,
    Attention,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Plan {
    Fit {
        w: Option<u32>,
        h: Option<u32>,
        allow_upscale: bool,
    },
    SmartCover {
        w: u32,
        h: u32,
        interest: Interest,
        allow_upscale: bool,
    },
    DirectionalCover {
        resize_w: u32,
        resize_h: u32,
        x: u32,
        y: u32,
        w: u32,
        h: u32,
    },
    Pad {
        fit_w: u32,
        fit_h: u32,
        canvas_w: u32,
        canvas_h: u32,
        off_x: u32,
        off_y: u32,
        allow_upscale: bool,
    },
}

pub fn plan(
    src_w: u32,
    src_h: u32,
    width: Option<u32>,
    height: Option<u32>,
    fit: FitMode,
    gravity: Gravity,
    focus: Option<Focus>,
) -> Plan {
    if src_w == 0 || src_h == 0 {
        return Plan::Fit {
            w: width,
            h: height,
            allow_upscale: false,
        };
    }

    match (fit, width, height) {
        (FitMode::Contain, _, _) => Plan::Fit {
            w: width,
            h: height,
            allow_upscale: true,
        },
        (FitMode::ScaleDown, _, _) => Plan::Fit {
            w: width,
            h: height,
            allow_upscale: false,
        },

        (FitMode::Cover, Some(w), Some(h)) => cover(src_w, src_h, w, h, gravity, focus, true),
        (FitMode::Crop, Some(w), Some(h)) => crop(src_w, src_h, w, h, gravity, focus),
        (FitMode::Pad, Some(w), Some(h)) => pad(src_w, src_h, w, h),

        (FitMode::Cover | FitMode::Pad, _, _) => Plan::Fit {
            w: width,
            h: height,
            allow_upscale: true,
        },
        (FitMode::Crop, _, _) => Plan::Fit {
            w: width,
            h: height,
            allow_upscale: false,
        },
    }
}

fn cover(
    src_w: u32,
    src_h: u32,
    w: u32,
    h: u32,
    gravity: Gravity,
    focus: Option<Focus>,
    allow_upscale: bool,
) -> Plan {
    if let Some(interest) = gravity.smart() {
        return Plan::SmartCover {
            w,
            h,
            interest,
            allow_upscale,
        };
    }
    let scale = (w as f64 / src_w as f64).max(h as f64 / src_h as f64);
    directional(src_w, src_h, w, h, scale, gravity, focus)
}

fn crop(src_w: u32, src_h: u32, w: u32, h: u32, gravity: Gravity, focus: Option<Focus>) -> Plan {
    let fill = (w as f64 / src_w as f64).max(h as f64 / src_h as f64);
    if fill > 1.0 {
        return Plan::Fit {
            w: Some(w),
            h: Some(h),
            allow_upscale: false,
        };
    }
    if let Some(interest) = gravity.smart() {
        return Plan::SmartCover {
            w,
            h,
            interest,
            allow_upscale: false,
        };
    }
    directional(src_w, src_h, w, h, fill, gravity, focus)
}

fn directional(
    src_w: u32,
    src_h: u32,
    w: u32,
    h: u32,
    scale: f64,
    gravity: Gravity,
    focus: Option<Focus>,
) -> Plan {
    let resize_w = ((src_w as f64 * scale).round() as u32).max(w);
    let resize_h = ((src_h as f64 * scale).round() as u32).max(h);
    let over_x = resize_w - w;
    let over_y = resize_h - h;

    let (x, y) = if gravity == Gravity::Face {
        face_anchor(resize_w, resize_h, w, h, focus)
    } else {
        let x = match gravity {
            Gravity::West | Gravity::NorthWest | Gravity::SouthWest => 0,
            Gravity::East | Gravity::NorthEast | Gravity::SouthEast => over_x,
            _ => over_x / 2,
        };
        let y = match gravity {
            Gravity::North | Gravity::NorthWest | Gravity::NorthEast => 0,
            Gravity::South | Gravity::SouthWest | Gravity::SouthEast => over_y,
            _ => over_y / 2,
        };
        (x, y)
    };

    Plan::DirectionalCover {
        resize_w,
        resize_h,
        x,
        y,
        w,
        h,
    }
}

fn face_anchor(resize_w: u32, resize_h: u32, w: u32, h: u32, focus: Option<Focus>) -> (u32, u32) {
    let over_x = resize_w - w;
    let over_y = resize_h - h;
    let Some(focus) = focus else {
        return (over_x / 2, 0);
    };
    let x = (focus.x * resize_w as f64 - w as f64 / 2.0).round();
    let y = (focus.y * resize_h as f64 - h as f64 * FACE_VERTICAL_BIAS).round();
    (
        x.clamp(0.0, over_x as f64) as u32,
        y.clamp(0.0, over_y as f64) as u32,
    )
}

fn pad(src_w: u32, src_h: u32, w: u32, h: u32) -> Plan {
    let (fit_w, fit_h) = contain_dims(src_w, src_h, Some(w), Some(h), true);
    Plan::Pad {
        fit_w,
        fit_h,
        canvas_w: w,
        canvas_h: h,
        off_x: (w.saturating_sub(fit_w)) / 2,
        off_y: (h.saturating_sub(fit_h)) / 2,
        allow_upscale: true,
    }
}

pub fn contain_dims(
    src_w: u32,
    src_h: u32,
    width: Option<u32>,
    height: Option<u32>,
    allow_upscale: bool,
) -> (u32, u32) {
    if src_w == 0 || src_h == 0 {
        return (src_w, src_h);
    }
    let scale = match (width, height) {
        (Some(w), Some(h)) => (w as f64 / src_w as f64).min(h as f64 / src_h as f64),
        (Some(w), None) => w as f64 / src_w as f64,
        (None, Some(h)) => h as f64 / src_h as f64,
        (None, None) => 1.0,
    };
    let scale = if allow_upscale { scale } else { scale.min(1.0) };
    let out_w = ((src_w as f64 * scale).round() as u32).max(1);
    let out_h = ((src_h as f64 * scale).round() as u32).max(1);
    (out_w, out_h)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scale_down_never_upscales() {
        let p = plan(
            800,
            600,
            Some(2000),
            Some(2000),
            FitMode::ScaleDown,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::Fit {
                w: Some(2000),
                h: Some(2000),
                allow_upscale: false
            }
        );
        assert_eq!(
            contain_dims(800, 600, Some(2000), Some(2000), false),
            (800, 600)
        );
    }

    #[test]
    fn contain_may_upscale_and_letterboxes_within_box() {
        assert_eq!(
            contain_dims(800, 600, Some(400), Some(400), true),
            (400, 300)
        );
        assert_eq!(
            contain_dims(400, 300, Some(800), Some(800), true),
            (800, 600)
        );
    }

    #[test]
    fn cover_center_uses_smartcrop() {
        let p = plan(
            800,
            600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::SmartCover {
                w: 200,
                h: 200,
                interest: Interest::Centre,
                allow_upscale: true
            }
        );
    }

    #[test]
    fn cover_auto_uses_attention() {
        let p = plan(
            800,
            600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Auto,
            None,
        );
        assert_eq!(
            p,
            Plan::SmartCover {
                w: 200,
                h: 200,
                interest: Interest::Attention,
                allow_upscale: true
            }
        );
    }

    #[test]
    fn cover_directional_north_anchors_top() {
        let p = plan(
            800,
            800,
            Some(400),
            Some(200),
            FitMode::Cover,
            Gravity::North,
            None,
        );
        assert_eq!(
            p,
            Plan::DirectionalCover {
                resize_w: 400,
                resize_h: 400,
                x: 0,
                y: 0,
                w: 400,
                h: 200
            }
        );
    }

    #[test]
    fn cover_directional_east_anchors_right() {
        let p = plan(
            800,
            400,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::East,
            None,
        );
        assert_eq!(
            p,
            Plan::DirectionalCover {
                resize_w: 400,
                resize_h: 200,
                x: 200,
                y: 0,
                w: 200,
                h: 200
            }
        );
    }

    #[test]
    fn crop_small_source_degrades_to_scale_down() {
        let p = plan(
            100,
            100,
            Some(400),
            Some(400),
            FitMode::Crop,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::Fit {
                w: Some(400),
                h: Some(400),
                allow_upscale: false
            }
        );
    }

    #[test]
    fn crop_large_source_fills_without_upscale() {
        let p = plan(
            800,
            600,
            Some(300),
            Some(300),
            FitMode::Crop,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::SmartCover {
                w: 300,
                h: 300,
                interest: Interest::Centre,
                allow_upscale: false
            }
        );
    }

    #[test]
    fn pad_centers_within_canvas() {
        let p = plan(
            800,
            600,
            Some(400),
            Some(400),
            FitMode::Pad,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::Pad {
                fit_w: 400,
                fit_h: 300,
                canvas_w: 400,
                canvas_h: 400,
                off_x: 0,
                off_y: 50,
                allow_upscale: true,
            }
        );
    }

    #[test]
    fn cover_missing_dimension_degrades_to_fit() {
        let p = plan(
            800,
            600,
            Some(300),
            None,
            FitMode::Cover,
            Gravity::Center,
            None,
        );
        assert_eq!(
            p,
            Plan::Fit {
                w: Some(300),
                h: None,
                allow_upscale: true
            }
        );
    }

    #[test]
    fn cover_face_without_focus_anchors_top() {
        let p = plan(
            1600,
            800,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Face,
            None,
        );
        assert_eq!(
            p,
            Plan::DirectionalCover {
                resize_w: 400,
                resize_h: 200,
                x: 100,
                y: 0,
                w: 200,
                h: 200
            }
        );
    }

    #[test]
    fn cover_face_frames_focus_with_headroom() {
        let p = plan(
            800,
            1600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Face,
            Some(Focus::new(0.5, 0.25)),
        );
        assert_eq!(
            p,
            Plan::DirectionalCover {
                resize_w: 200,
                resize_h: 400,
                x: 0,
                y: 24,
                w: 200,
                h: 200
            }
        );
    }

    #[test]
    fn cover_face_focus_clamps_to_bounds() {
        let high = plan(
            800,
            1600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Face,
            Some(Focus::new(0.5, 0.05)),
        );
        let low = plan(
            800,
            1600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Face,
            Some(Focus::new(0.5, 0.98)),
        );
        assert!(matches!(high, Plan::DirectionalCover { y: 0, .. }));
        assert!(matches!(low, Plan::DirectionalCover { y: 200, .. }));
    }

    #[test]
    fn face_no_longer_uses_attention() {
        let p = plan(
            800,
            600,
            Some(200),
            Some(200),
            FitMode::Cover,
            Gravity::Face,
            None,
        );
        assert!(!matches!(p, Plan::SmartCover { .. }));
    }
}
