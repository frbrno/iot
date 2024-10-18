#![allow(dead_code)]
#![allow(unused)]

use anyhow::{
	Result,
	anyhow,
};
use embedded_svc::{
	http::{
		Headers,
		Method,
		client::Client,
	},
	wifi::{
		AuthMethod,
		ClientConfiguration,
		Configuration,
	},
};
use esp_idf_svc::{
	eventloop::EspSystemEventLoop,
	hal::{
		delay,
		gpio::PinDriver,
		modem,
		peripherals::Peripherals,
		prelude::*,
	},
	http::client::EspHttpConnection,
	mqtt::client::*,
	nvs::EspDefaultNvsPartition,
	ota::{
		EspFirmwareInfoLoader,
		EspOta,
		FirmwareInfo,
	},
	sys,
	sys::{
		ESP_ERR_IMAGE_INVALID,
		ESP_ERR_INVALID_RESPONSE,
		EspError,
		esp_wifi_set_max_tx_power,
	},
	timer::EspTaskTimerService,
	wifi::*,
};
use event::ActionReply;
use log::*;
use serde_json::json;
use uln2003::{
	StepperMotor,
	ULN2003,
};

mod event;
mod handler;
mod otaupdate;
mod stepper;

const FIRMWARE_DOWNLOAD_CHUNK_SIZE: usize = 1024 * 20;
const FIRMWARE_MAX_SIZE: usize = 1024 * 1024 * 2;
const FIRMWARE_MIN_SIZE: usize = size_of::<FirmwareInfo>() + 1024;

const SSID: &str = env!("WIFI_SSID");
const PASSWORD: &str = env!("WIFI_PASS");

const HELLO: &str = "HiThere69";

const MQTT_URL: &str = "mqtt://192.168.10.124:1880";
const MQTT_CLIENT_ID: &str = "rusty_falcon";
const MQTT_TOPIC: &str = "+/rusty_falcon/+/+/+";
// {src}.{dst}.stepper1_speed.{get,set,run}.{token}
// {src}.{dst}.stepper1_speed.{ack,cancel,done,error}.{token}

pub struct Stepper<T: uln2003::StepperMotor> {
	ext: T,
	pub position_current: usize,
	pub direction: uln2003::Direction,
}

impl<T: uln2003::StepperMotor> Stepper<T> {
	pub fn new(ext: T) -> Self {
		Stepper {
			ext,
			position_current: 0,
			direction: uln2003::Direction::Normal, //in my case 'right'
		}
	}

	// ~190000 steps for my 2m linear rail
	pub fn step(&mut self) -> Result<(), uln2003::StepError> {
		match self.direction {
			uln2003::Direction::Reverse => {
				if self.position_current > 0 {
					self.position_current -= 1;
				}
			}
			uln2003::Direction::Normal => {
				self.position_current += 1;
			}
		}
		self.ext.step() //TODO error?
	}
	/// Do multiple steps with a given delay in ms
	pub fn step_for(&mut self, steps: i32, delay: u32) -> Result<(), uln2003::StepError> {
		self.ext.step_for(steps, delay)
	}
	/// Set the stepping direction
	pub fn set_direction(&mut self, dir: uln2003::Direction) {
		match dir {
			uln2003::Direction::Normal => self.direction = uln2003::Direction::Normal,
			uln2003::Direction::Reverse => self.direction = uln2003::Direction::Reverse,
		}
		self.ext.set_direction(dir)
	}
	/// Stoping sets all pins low
	pub fn stop(&mut self) -> Result<(), uln2003::StepError> {
		self.ext.stop()
	}
}
fn main() {
	esp_idf_svc::sys::link_patches();
	esp_idf_svc::log::EspLogger::initialize_default();
	unsafe { esp_wifi_set_max_tx_power(66) };

	info!("{:?}", HELLO);
	info!("ssid: {:?}, pw: {:?}", SSID, PASSWORD);
	let peripherals = esp_idf_svc::hal::peripherals::Peripherals::take().unwrap();

	let sys_loop = EspSystemEventLoop::take().unwrap();
	let nvs = EspDefaultNvsPartition::take().unwrap();

	let mut wifi = BlockingWifi::wrap(
		EspWifi::new(peripherals.modem, sys_loop.clone(), Some(nvs)).unwrap(),
		sys_loop.clone(),
	)
	.unwrap();

	connect_wifi(&mut wifi);

	let ip_info = wifi.wifi().sta_netif().get_ip_info().unwrap();

	info!("Wifi DHCP info: {:?}", ip_info);

	match wifi.is_connected() {
		Ok(is) => {
			if !is {
				unsafe {
					esp_idf_svc::sys::esp_restart();
				}
			}
		}
		Err(_) => unsafe {
			esp_idf_svc::sys::esp_restart();
		},
	}

	let (mut client, mut conn) = mqtt_create(MQTT_URL, MQTT_CLIENT_ID).unwrap();

	let stepper1 = std::sync::Arc::new(std::sync::Mutex::new(stepper::Stepper::new(Box::new(
		ULN2003::new(
			PinDriver::output(peripherals.pins.gpio9).unwrap(),
			PinDriver::output(peripherals.pins.gpio8).unwrap(),
			PinDriver::output(peripherals.pins.gpio7).unwrap(),
			PinDriver::output(peripherals.pins.gpio6).unwrap(),
			Some(delay::Delay::new_default()),
		),
	))));

	let timer_service = EspTaskTimerService::new().unwrap();

	let (tx_worker, rx_worker) = flume::unbounded();
	let (tx_event, rx_event) = flume::unbounded();

	let tx_worker_from_eventer = tx_worker.clone();
	let tx_eventer_from_worker = tx_event.clone();
	let tx_eventer_from_mqtt = tx_event.clone();

	let p2p_token = std::sync::Arc::new(std::sync::Mutex::new(0i64));

	let mut handler = handler::Handler::new(
		p2p_token.clone(),
		stepper1.clone(),
		tx_eventer_from_worker.clone(),
		rx_worker.clone(),
		timer_service.clone(),
	);
	handler::say_hello();

	let _subscription = sys_loop.subscribe::<WifiEvent, _>(move |event| {
		info!("[Subscribe callback] Got event: {:?}", event);
		match event {
			WifiEvent::StaDisconnected => {
				info!("******* Received STA Disconnected event");
				if let Err(err) = wifi.connect() {
					info!("Error calling wifi.connect in wifi reconnect {:?}", err);
				}
			}
			_ => info!("other sysloop event {:?}", event),
		}
	});

	std::thread::scope(|s| {
		s.spawn(|| {
			std::thread::Builder::new()
				.stack_size(6000)
				.spawn_scoped(s, move || {
					info!("MQTT Listening for messages");

					while let Ok(event) = conn.next() {
						info!("[Queue] Event: {}", event.payload());
						match event.payload() {
							EventPayload::Received { topic, data, .. } => {
								if let Some(topic) = topic {
									match event::action_from_mqtt(topic, data) {
										Ok((e, ctx)) => {
											let _ = tx_eventer_from_mqtt.send(event::Event::Action((e, ctx)));
										}
										Err(err) => {
											info!("mqtt msg handler err: {:?}", err);
										}
									}
								}
							}
							_ => {
								info!("Other event: {:?}", event.payload());
							}
						}
					}
					info!("Connection closed");
				})
				.unwrap();
		});
		// eventer
		s.spawn(move || {
			info!("spawn eventer");
			loop {
				if let Err(e) = client.subscribe(MQTT_TOPIC, QoS::AtMostOnce) {
					error!("Failed to subscribe to topic \"{MQTT_TOPIC}\": {e}, retrying...");

					// Re-try in 0.5s
					std::thread::sleep(std::time::Duration::from_millis(500));

					continue;
				}

				info!("Subscribed to topic \"{MQTT_TOPIC}\"");
				break;
			}
			client.publish(
				format!("rusty_falcon/hello/{:}", HELLO.to_string()).as_str(),
				QoS::AtMostOnce,
				false,
				&[0],
			);
			let mut fn_action_reply = |reply: event::ActionReply, ctx: event::Context| {
				let topic = reply.topic(&ctx).to_string();
				match reply.data() {
					Some(data) => match serde_json::to_string(&data) {
						Ok(data) => {
							client.publish(topic.as_str(), QoS::AtMostOnce, false, data.as_bytes());
						}
						Err(err) => {
							// serialization failed
							let reply = event::ActionReply::Error {
								data: Some(
									(event::ReplyData::WithString {
										message: err.to_string(),
									}),
								),
							};
							let data = serde_json::to_string(reply.data()).unwrap();
							client.publish(
								reply.topic(&ctx).as_str(),
								QoS::AtMostOnce,
								false,
								data.as_bytes(),
							);
						}
					},
					None => {
						client.publish(topic.as_str(), QoS::AtMostOnce, false, &[0]);
					}
				}
			};

			loop {
				let mut event = rx_event.recv().unwrap();
				match event {
					event::Event::Action((action, ctx)) => {
						let _ = tx_worker_from_eventer.send((action, ctx));
					}
					event::Event::ActionReply((reply, ctx)) => {
						fn_action_reply(reply, ctx);
					}

					_ => std::panic!("not handled yet"),
				}
			}
		});
		// worker
		let p2p_token = std::sync::Arc::clone(&p2p_token);
		s.spawn(|| {
			std::thread::Builder::new()
				.stack_size(4 * 1024)
				.spawn_scoped(s, move || {
					info!("spawn worker");
					let mut timeout = std::time::Duration::from_millis(5);

					let fn_handle_get = |action_get: &event::ActionGet, ctx: &event::Context| match action_get {
						event::ActionGet::Stepper1State() => {
							let guard = stepper1.lock().unwrap();
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

							tx_eventer_from_worker.send(event::reply_ack(Some(data), ctx));
						}
						event::ActionGet::P2PToken() => {
							let guard = p2p_token.lock().unwrap();
							let token = *guard;
							drop(guard);

							let data = event::ReplyData::P2PToken { p2p_token: token };
							tx_eventer_from_worker.send(event::reply_ack(Some(data), ctx));
						}
					};

					let mut fn_handle_run =
						|action_run: &event::ActionRun, ctx: &event::Context, rx_cancel: flume::Receiver<bool>| {
							match action_run {
								event::ActionRun::P2PInit { ref data } => {
									handler.run_p2p_init(data, ctx, rx_cancel);
								}
								event::ActionRun::UpdateBoard { ref data } => {
									handler.run_update_board(data, ctx, rx_cancel);
								}
								event::ActionRun::Stepper1MoveTo { ref data } => {
									handler.run_stepper1_move_to(data, ctx, rx_cancel);
								}
								event::ActionRun::Stepper1SpeedLeft { ref data } => {
									handler.run_stepper1_speed(data, ctx, uln2003::Direction::Normal, rx_cancel);
								}
								event::ActionRun::Stepper1SpeedRight { ref data } => {
									handler.run_stepper1_speed(data, ctx, uln2003::Direction::Reverse, rx_cancel);
								}
								event::ActionRun::Stop() => {
									rx_cancel.recv(); // just hang here until next run commands comes
								}
								_ => tx_eventer_from_worker
									.send(event::reply_error(
										String::from("fn_handle_run err: no handler impl"),
										ctx,
									))
									.unwrap(),
							}
						};

					'loop_recv: loop {
						let (mut action, mut ctx) = rx_worker.recv().unwrap();
						'loop_match: loop {
							match action {
								event::Action::ActionGet((ref action_get)) => {
									fn_handle_get(action_get, &ctx);
									continue;
								}
								event::Action::ActionRun((ref action_run)) => {
									let (tx_cancel, rx_cancel) = flume::bounded(1);
									let mut recv_next: Option<(event::Action, event::Context)> = None;
									std::thread::scope(|s| {
										s.spawn(|| {
											std::thread::Builder::new()
												.stack_size(4 * 1024)
												.spawn_scoped(s, || {
													fn_handle_run(action_run, &ctx, rx_cancel);
												})
										});
										s.spawn(|| {
											loop {
												let (action_next, ctx_next) = rx_worker.recv().unwrap();
												match action_next {
													event::Action::ActionGet((ref action_get)) => {
														// while action_run is active, answer to simple action_get commands
														fn_handle_get(action_get, &ctx_next);
														continue;
													}
													_ => {
														// got action_set/run, this terminates the active action_run command
														recv_next = Some((action_next, ctx_next));
														//tx_cancel.try_send(true);
														drop(tx_cancel); //this is like close(channel) in go, multiple threads get the signal
														break;
													}
												}
											}
										});
									});
									if let Some((action_next, ctx_next)) = recv_next {
										action = action_next;
										ctx = ctx_next;
										continue 'loop_match;
									}
									continue 'loop_recv;
								}
							}
						}
					}
				})
				.unwrap();
		});
	});
}

fn mqtt_create(
	url: &str,
	client_id: &str,
) -> Result<(EspMqttClient<'static>, EspMqttConnection), EspError> {
	let (mqtt_client, mqtt_conn) = EspMqttClient::new(url, &MqttClientConfiguration {
		client_id: Some(client_id),
		disable_clean_session: true,

		..Default::default()
	})?;

	Ok((mqtt_client, mqtt_conn))
}

fn connect_wifi(wifi: &mut BlockingWifi<EspWifi<'static>>) -> anyhow::Result<()> {
	let wifi_configuration: Configuration = Configuration::Client(ClientConfiguration {
		ssid: SSID.try_into().unwrap(),
		bssid: None,
		auth_method: AuthMethod::WPA2Personal,
		password: PASSWORD.try_into().unwrap(),
		channel: None,
		..Default::default()
	});

	wifi.set_configuration(&wifi_configuration)?;

	wifi.start()?;
	info!("Wifi started");

	wifi.connect()?;
	info!("Wifi connected");

	wifi.wait_netif_up()?;
	info!("Wifi netif up");

	Ok(())
}
