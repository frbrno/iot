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

#[derive(Clone)]
pub struct Handler {
	pub p2p_token: Arc<Mutex<i64>>,
	pub stepper1: Arc<Mutex<stepper::Stepper>>,
	tx_event: flume::Sender<(event::Event)>,
	rx_event: flume::Receiver<(event::Cmd, event::Context)>,
	timer_service: EspTimerService<Task>,
}

impl Handler {
	pub fn new(
		stepper1: Arc<Mutex<stepper::Stepper>>,
		tx_event: flume::Sender<(event::Event)>,
		rx_event: flume::Receiver<(event::Cmd, event::Context)>,
		timer_service: EspTimerService<Task>,
	) -> Self {
		Handler {
			p2p_token: Arc::new(Mutex::new(0)),
			stepper1,
			tx_event,
			rx_event,
			timer_service,
		}
	}

	pub fn run_stop(&self, ctx: &event::Context, rx_cancel: flume::Receiver<bool>) {
		self.tx_event.send(event::reply_ack(None, ctx));
		self.tx_event.send(event::reply_done(None, ctx));
		rx_cancel.recv(); // just hang here until next run commands comes
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

	pub fn handle_loop(&mut self) {
		let mut fn_handle_get = {
			let mut this = self.clone();
			move |cmd_get: &event::CmdGet, ctx: &event::Context| match cmd_get {
				event::CmdGet::Stepper1State() => {
					let guard = this.stepper1.lock().unwrap();
					let mut direction = "";
					match guard.direction {
						uln2003::Direction::Normal => direction = "left",
						uln2003::Direction::Reverse => direction = "right",
					}
					let mut data = event::ReplyData::Stepper1State {
						position_current: guard.position_current,
						direction: direction.to_string(),
					};
					drop(guard);

					this.tx_event.send(event::reply_ack(Some(data), ctx));
				}
				event::CmdGet::P2PToken() => {
					let guard = this.p2p_token.lock().unwrap();
					let token = *guard;
					drop(guard);

					let data = event::ReplyData::P2PToken { p2p_token: token };
					this.tx_event.send(event::reply_ack(Some(data), ctx));
				}
			}
		};

		let mut fn_handle_run = {
			let mut this = self.clone();
			move |cmd_run: &event::CmdRun, ctx: &event::Context, rx_cancel: flume::Receiver<bool>| {
				match cmd_run {
					event::CmdRun::P2PInit { ref data } => {
						this.run_p2p_init(data, ctx, rx_cancel);
					}
					event::CmdRun::UpdateBoard { ref data } => {
						this.run_update_board(data, ctx, rx_cancel);
					}
					event::CmdRun::Stepper1MoveTo { ref data } => {
						this.run_stepper1_move_to(data, ctx, rx_cancel);
					}
					event::CmdRun::Stepper1SpeedLeft { ref data } => {
						this.run_stepper1_speed(data, ctx, uln2003::Direction::Normal, rx_cancel);
					}
					event::CmdRun::Stepper1SpeedRight { ref data } => {
						this.run_stepper1_speed(data, ctx, uln2003::Direction::Reverse, rx_cancel);
					}
					event::CmdRun::Stop() => {
						this.run_stop(ctx, rx_cancel);
					}
					_ => this
						.tx_event
						.send(event::reply_error(
							String::from("fn_handle_run err: no handler impl"),
							ctx,
						))
						.unwrap(),
				}
			}
		};
		'loop_recv: loop {
			let (mut cmd, mut ctx) = self.rx_event.recv().unwrap();
			'loop_match: loop {
				match cmd {
					event::Cmd::CmdGet((ref cmd_get)) => {
						fn_handle_get(cmd_get, &ctx);
						continue 'loop_recv;
					}
					event::Cmd::CmdRun((ref cmd_run)) => {
						let (tx_cancel, rx_cancel) = flume::bounded(1);
						let mut recv_next: Option<(event::Cmd, event::Context)> = None;
						std::thread::scope(|s| {
							s.spawn(|| {
								std::thread::Builder::new()
									.stack_size(4 * 1024)
									.spawn_scoped(s, || {
										fn_handle_run(cmd_run, &ctx, rx_cancel);
									})
							});
							s.spawn(|| {
								loop {
									let (cmd_next, ctx_next) = self.rx_event.recv().unwrap();
									match cmd_next {
										event::Cmd::CmdGet((ref cmd_get)) => {
											// while cmd_run is active, answer to simple cmd_get commands
											fn_handle_get(cmd_get, &ctx_next);
											continue;
										}
										_ => {
											// got cmd_set/run, this terminates the active cmd_run command
											recv_next = Some((cmd_next, ctx_next));
											//tx_cancel.try_send(true);
											drop(tx_cancel); //this is like close(channel) in go, multiple listener get the signal
											break;
										}
									}
								}
							});
						});
						if let Some((cmd_next, ctx_next)) = recv_next {
							cmd = cmd_next;
							ctx = ctx_next;
							continue 'loop_match;
						}
						continue 'loop_recv;
					}
				}
			}
		}
	}
}
