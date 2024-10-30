use std::{
	env,
	fs,
	io::Write,
};

fn main() {
	embuild::espidf::sysenv::output();
	let outdir = env::var("OUT_DIR").unwrap();
	let outfile = format!("{}/timestamp.txt", outdir);

	let mut fh = fs::File::create(&outfile).unwrap();
	write!(fh, r#""{}""#, chrono::Local::now()).ok();
}
