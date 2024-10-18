use crate::{
	event,
	otaupdate,
	stepper,
};
use esp_idf_svc::timer::{
	EspTimerService,
	Task,
};
use flume;
use std::sync::{
	Arc,
	Mutex,
};
pub fn say_hello() {
	println!("hello");
}
pub struct Handler {
	pub p2p_token: Arc<Mutex<i64>>,
	pub stepper1: Arc<Mutex<stepper::Stepper>>,
	tx_event: flume::Sender<(event::Event)>,
	rx_event: flume::Receiver<(event::Cmd, event::Context)>,
	timer_service: EspTimerService<Task>,
}

impl Handler {
	pub fn new(
		p2p_token: Arc<Mutex<i64>>,
		stepper1: Arc<Mutex<stepper::Stepper>>,
		tx_event: flume::Sender<(event::Event)>,
		rx_event: flume::Receiver<(event::Cmd, event::Context)>,
		timer_service: EspTimerService<Task>,
	) -> Self {
		Handler {
			p2p_token,
			stepper1,
			tx_event,
			rx_event,
			timer_service,
		}
	}

	pub fn run_stepper1_move_to(
		&self,
		data: &event::RequestStepper1MoveTo,
		ctx: &event::Context,
		rx_cancel: flume::Receiver<bool>,
	) {
		self.tx_event.send(event::reply_ack(None, ctx));
		let step_delay = std::time::Duration::from_micros(data.step_delay_micros as u64);

		let mut guard = self.stepper1.lock().unwrap();
		if data.position_final == guard.position_current {
			drop(guard);
			self.tx_event.send(event::reply_done(None, ctx));
			rx_cancel.recv();
			return;
		} else if data.position_final > guard.position_current {
			// move to left
			guard.set_direction(uln2003::Direction::Normal);
		} else {
			// move to right
			guard.set_direction(uln2003::Direction::Reverse);
		}
		let mut position_abs_diff = guard.position_current.abs_diff(data.position_final);
		drop(guard);

		let (tx_done, rx_done) = flume::bounded::<bool>(1);

		let timer = unsafe {
			let stepper1 = std::sync::Arc::clone(&self.stepper1);
			let mut done = false;
			self
				.timer_service
				.timer_nonstatic(move || {
					if !done {
						let mut guard = stepper1.lock().unwrap();
						guard.step();

						let position_abs_diff_next = guard.position_current.abs_diff(data.position_final);

						if position_abs_diff_next == 0 {
							done = true;
							tx_done.try_send(true);
							guard.stop();
							drop(guard);
							return;
						}
						if position_abs_diff_next > position_abs_diff {
							// something wrong, it should get smaller
							done = true;
							tx_done.try_send(false);
							guard.stop();
							drop(guard);
							return;
						}

						drop(guard);

						position_abs_diff = position_abs_diff_next;
					}
				})
				.unwrap()
		};

		timer.every(step_delay).unwrap();

		flume::Selector::new()
			.recv(&rx_done, |success| {
				let success = success.unwrap_or(false);
				if success {
					self.tx_event.send(event::reply_done(None, ctx));
				} else {
					self.tx_event.send(event::reply_error(
						String::from("failed; guess logical error; move left right position_current and final etc, gets confusing. This needs a fix!!"),
						ctx,
					));
				}
			})
			.recv(&rx_cancel, |e| {
				self.tx_event.send(event::reply_cancel(None, ctx));
			})
			.wait();

		timer.cancel();
		drop(timer);
	}

	pub fn run_update_board(
		&mut self,
		data: &event::DataReqRunUpdateBoard,
		ctx: &event::Context,
		rx_cancel: flume::Receiver<bool>,
	) {
		self.tx_event.send(event::reply_ack(None, ctx));
		match otaupdate::simple_download_and_update_firmware(data.url.clone()) {
			Ok(_) => {
				self.tx_event.send(event::reply_done(None, ctx));
			}
			Err(err) => {
				self.tx_event.send(event::reply_error(err.to_string(), ctx));
			}
		}
		//wait result message is out
		std::thread::sleep(std::time::Duration::from_secs(2));
		unsafe {
			esp_idf_svc::sys::esp_restart();
		}
	}

	pub fn run_p2p_init(
		&mut self,
		data: &event::RequestP2PInit,
		ctx: &event::Context,
		rx_cancel: flume::Receiver<bool>,
	) {
		self.tx_event.send(event::reply_ack(None, ctx));
		let mut guard = self.p2p_token.lock().unwrap();
		*guard = data.p2p_token;
		drop(guard);
		self.tx_event.send(event::reply_done(None, ctx));
	}

	pub fn run_stepper1_speed(
		&mut self,
		data: &event::DataReqRunStepper1Speed,
		ctx: &event::Context,
		dir: uln2003::Direction,
		rx_cancel: flume::Receiver<bool>,
	) {
		self.tx_event.send(event::reply_ack(None, ctx));

		let step_delay = std::time::Duration::from_micros(data.speed);
		let mut stepper_guard = self.stepper1.lock().unwrap();
		stepper_guard.set_direction(dir);
		drop(stepper_guard);

		let timer = unsafe {
			let stepper1 = std::sync::Arc::clone(&self.stepper1);
			self
				.timer_service
				.timer_nonstatic(move || {
					let mut stepper_guard = stepper1.lock().unwrap();
					stepper_guard.step();
					drop(stepper_guard);
				})
				.unwrap()
		};

		timer.every(step_delay).unwrap();

		let mut result: Option<(event::Cmd, event::Context)> = None;

		rx_cancel.recv();

		timer.cancel();
		drop(timer);

		let mut stepper_guard = self.stepper1.lock().unwrap();
		stepper_guard.stop();
		drop(stepper_guard);

		self.tx_event.send(event::Event::CmdReply((
			event::CmdReply::Cancel(),
			ctx.clone(),
		)));
	}
}
