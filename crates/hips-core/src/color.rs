#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Rgba {
    pub r: u8,
    pub g: u8,
    pub b: u8,
    pub a: u8,
}

impl Rgba {
    pub const WHITE: Rgba = Rgba {
        r: 255,
        g: 255,
        b: 255,
        a: 255,
    };
    pub const BLACK: Rgba = Rgba {
        r: 0,
        g: 0,
        b: 0,
        a: 255,
    };
    pub const TRANSPARENT: Rgba = Rgba {
        r: 0,
        g: 0,
        b: 0,
        a: 0,
    };

    pub fn parse(input: &str) -> Option<Rgba> {
        let s = input.trim();
        match s.to_ascii_lowercase().as_str() {
            "white" => return Some(Rgba::WHITE),
            "black" => return Some(Rgba::BLACK),
            "transparent" | "none" => return Some(Rgba::TRANSPARENT),
            _ => {}
        }

        let hex = s.strip_prefix('#').unwrap_or(s).as_bytes();
        match hex.len() {
            3 => Some(Rgba {
                r: expand(hex_nibble(hex[0])?),
                g: expand(hex_nibble(hex[1])?),
                b: expand(hex_nibble(hex[2])?),
                a: 255,
            }),
            4 => Some(Rgba {
                r: expand(hex_nibble(hex[0])?),
                g: expand(hex_nibble(hex[1])?),
                b: expand(hex_nibble(hex[2])?),
                a: expand(hex_nibble(hex[3])?),
            }),
            6 => Some(Rgba {
                r: hex_byte(hex[0], hex[1])?,
                g: hex_byte(hex[2], hex[3])?,
                b: hex_byte(hex[4], hex[5])?,
                a: 255,
            }),
            8 => Some(Rgba {
                r: hex_byte(hex[0], hex[1])?,
                g: hex_byte(hex[2], hex[3])?,
                b: hex_byte(hex[4], hex[5])?,
                a: hex_byte(hex[6], hex[7])?,
            }),
            _ => None,
        }
    }

    pub fn is_opaque(self) -> bool {
        self.a == 255
    }
}

fn hex_nibble(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        b'A'..=b'F' => Some(c - b'A' + 10),
        _ => None,
    }
}

fn hex_byte(hi: u8, lo: u8) -> Option<u8> {
    Some(hex_nibble(hi)? << 4 | hex_nibble(lo)?)
}

fn expand(nibble: u8) -> u8 {
    nibble << 4 | nibble
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_named() {
        assert_eq!(Rgba::parse("white"), Some(Rgba::WHITE));
        assert_eq!(Rgba::parse(" Black "), Some(Rgba::BLACK));
        assert_eq!(Rgba::parse("transparent"), Some(Rgba::TRANSPARENT));
    }

    #[test]
    fn parses_hex_short() {
        assert_eq!(Rgba::parse("#fff"), Some(Rgba::WHITE));
        assert_eq!(
            Rgba::parse("#000f"),
            Some(Rgba {
                r: 0,
                g: 0,
                b: 0,
                a: 255
            })
        );
    }

    #[test]
    fn parses_hex_long() {
        assert_eq!(
            Rgba::parse("#ff8800"),
            Some(Rgba {
                r: 255,
                g: 136,
                b: 0,
                a: 255
            })
        );
        assert_eq!(
            Rgba::parse("00ff0080"),
            Some(Rgba {
                r: 0,
                g: 255,
                b: 0,
                a: 128
            })
        );
    }

    #[test]
    fn rejects_garbage() {
        assert_eq!(Rgba::parse("#zz"), None);
        assert_eq!(Rgba::parse("rgb(1,2,3)"), None);
    }
}
