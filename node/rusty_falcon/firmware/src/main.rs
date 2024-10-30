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
use event::CmdReply;
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

pub const BUILD_TIME: &str = include!(concat!(env!("OUT_DIR"), "/timestamp.txt"));
const MQTT_URL: &str = "mqtt://192.168.10.130:1880";
const MQTT_CLIENT_ID: &str = "rusty_falcon_1_mcu";
const MQTT_TOPIC: &str = "iot/rusty_falcon_1_mcu/rx/+/+/exec/+/+";
// {src}.{dst}.stepper1_speed.{get,set,run}.{token}
// {src}.{dst}.stepper1_speed.{ack,cancel,done,error}.{token}

fn main() {
	esp_idf_svc::sys::link_patches();
	esp_idf_svc::log::EspLogger::initialize_default();
	unsafe { esp_wifi_set_max_tx_power(64) };

	info!("build time {:?}", BUILD_TIME);
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

	//let p2p_token = std::sync::Arc::new(std::sync::Mutex::new(0i64));

	let mut handler = handler::Handler::new(
		stepper1.clone(),
		tx_eventer_from_worker.clone(),
		rx_worker.clone(),
		timer_service.clone(),
	);

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
									// TODO: only a command if its a get/set/run event_typ
									match event::from_mqtt(topic, data) {
										Ok((e, ctx)) => {
											let _ = tx_eventer_from_mqtt.send(event::Event::Cmd((e, ctx)));
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
				format!("iot/rusty_falcon_1_mcu/tx/broadcast/event/exec/iamalive/0").as_str(),
				QoS::AtMostOnce,
				false,
				&[0],
			);
			let mut fn_cmd_reply = |reply: event::CmdReply, ctx: event::Context| {
				let topic = reply.topic(&ctx).to_string();
				match reply.data() {
					Some(data) => match serde_json::to_string(&data) {
						Ok(data) => {
							client.publish(topic.as_str(), QoS::AtMostOnce, false, data.as_bytes());
						}
						Err(err) => {
							// serialization failed
							let reply = event::CmdReply::Error {
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
					event::Event::Cmd((cmd, ctx)) => {
						let _ = tx_worker_from_eventer.send((cmd, ctx));
					}
					event::Event::CmdReply((reply, ctx)) => {
						fn_cmd_reply(reply, ctx);
					}

					_ => {
						println!("about to panic rx_event");
						std::thread::sleep(std::time::Duration::from_secs(2));
						std::panic!("not handled yet")
					}
				}
			}
		});

		s.spawn(|| {
			std::thread::Builder::new()
				.stack_size(4 * 1024)
				.spawn_scoped(s, || {
					handler.handle_loop();
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
