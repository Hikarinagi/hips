fn main() {
    if let Err(err) = pkg_config::Config::new().probe("vips") {
        println!("cargo:warning=pkg-config could not locate libvips: {err}");
    }
}
