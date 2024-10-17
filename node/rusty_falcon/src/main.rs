#![allow(dead_code)]
#![allow(unused)]

use anyhow::{anyhow, Result};
use embedded_svc::{
    http::{client::Client, Headers, Method},
    wifi::{AuthMethod, ClientConfiguration, Configuration},
};
use esp_idf_svc::{
    eventloop::EspSystemEventLoop,
    hal::{delay, gpio::PinDriver, modem, peripherals::Peripherals, prelude::*},
    http::client::EspHttpConnection,
    mqtt::client::*,
    nvs::EspDefaultNvsPartition,
    ota::{EspFirmwareInfoLoader, EspOta, FirmwareInfo},
    sys,
    sys::{esp_wifi_set_max_tx_power, EspError, ESP_ERR_IMAGE_INVALID, ESP_ERR_INVALID_RESPONSE},
    timer::EspTaskTimerService,
    wifi::*,
};
use event::ActionReply;
use log::*;
use serde_json::json;
use uln2003::{StepperMotor, ULN2003};

mod event;

const FIRMWARE_DOWNLOAD_CHUNK_SIZE: usize = 1024 * 20;
const FIRMWARE_MAX_SIZE: usize = 1024 * 1024 * 2;
const FIRMWARE_MIN_SIZE: usize = size_of::<FirmwareInfo>() + 1024;

const SSID: &str = env!("WIFI_SSID");
const PASSWORD: &str = env!("WIFI_PASS");

const HELLO: &str = "HiThere11";

const MQTT_URL: &str = "mqtt://192.168.10.124:1880";
const MQTT_CLIENT_ID: &str = "rusty_falcon";
const MQTT_TOPIC: &str = "+/rusty_falcon/+/+/+";
// {src}.{dst}.stepper1_speed.{get,set,run}.{token}
// {src}.{dst}.stepper1_speed.{ack,cancel,done,error}.{token}

pub struct Stepper<T: uln2003::StepperMotor> {
    ext: T,
    position_current: usize,
    direction: uln2003::Direction,
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
    fn step(&mut self) -> Result<(), uln2003::StepError> {
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
    fn step_for(&mut self, steps: i32, delay: u32) -> Result<(), uln2003::StepError> {
        self.ext.step_for(steps, delay)
    }
    /// Set the stepping direction
    fn set_direction(&mut self, dir: uln2003::Direction) {
        match dir {
            uln2003::Direction::Normal => self.direction = uln2003::Direction::Normal,
            uln2003::Direction::Reverse => self.direction = uln2003::Direction::Reverse,
        }
        self.ext.set_direction(dir)
    }
    /// Stoping sets all pins low
    fn stop(&mut self) -> Result<(), uln2003::StepError> {
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

    let stepper1 = std::sync::Arc::new(std::sync::Mutex::new(Stepper::new(ULN2003::new(
        PinDriver::output(peripherals.pins.gpio9).unwrap(),
        PinDriver::output(peripherals.pins.gpio8).unwrap(),
        PinDriver::output(peripherals.pins.gpio7).unwrap(),
        PinDriver::output(peripherals.pins.gpio6).unwrap(),
        Some(delay::Delay::new_default()),
    ))));

    let timer_service = EspTaskTimerService::new().unwrap();

    let (tx_worker, rx_worker) = flume::unbounded();
    let (tx_event, rx_event) = flume::unbounded();

    let tx_worker_from_eventer = tx_worker.clone();
    let tx_eventer_from_worker = tx_event.clone();
    let tx_eventer_from_mqtt = tx_event.clone();

    let p2p_token = std::sync::Arc::new(std::sync::Mutex::new(0i64));


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
        s.spawn(|| {});
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
                                            let _ = tx_eventer_from_mqtt
                                                .send(event::Event::Action((e, ctx)));
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
            client.enqueue(
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
                            client.enqueue(topic.as_str(), QoS::AtMostOnce, false, data.as_bytes());
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
                            client.enqueue(
                                reply.topic(&ctx).as_str(),
                                QoS::AtMostOnce,
                                false,
                                data.as_bytes(),
                            );
                        }
                    },
                    None => {
                        client.enqueue(topic.as_str(), QoS::AtMostOnce, false, &[0]);
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
        //let stepper1_shared = std::sync::Arc::clone(&stepper1_shared);

        let p2p_token=std::sync::Arc::clone(&p2p_token);
        s.spawn(|| {
            std::thread::Builder::new()
                .stack_size(4 * 1024)
                .spawn_scoped(s, move || {
                    info!("spawn worker");
                    let mut timeout = std::time::Duration::from_millis(5);

                    let fn_handle_get =
                        |action_get: event::ActionGet, ctx: event::Context| match action_get {
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

                                tx_eventer_from_worker
                                    .send(event::reply_ack(Some(data), ctx.clone()));
                            }
                            event::ActionGet::P2PToken() => {
                                let guard= p2p_token.lock().unwrap();
                                let token = *guard;
                                drop(guard);

                                let data = event::ReplyData::P2PToken { p2p_token: token };
                                tx_eventer_from_worker
                                    .send(event::reply_ack(Some(data), ctx.clone()));
                            }
                        };

                    let fn_stepper1_move_to =
                        |data: &event::RequestStepper1MoveTo,
                         ctx: &event::Context|
                         -> Option<(event::Action, event::Context)> {
                            tx_eventer_from_worker.send(event::reply_ack(None, ctx.clone()));
                            let step_delay =
                                std::time::Duration::from_micros(data.step_delay_micros as u64);

                            let mut guard = stepper1.lock().unwrap();
                            if data.position_final == guard.position_current {
                                drop(guard);
                                tx_eventer_from_worker.send(event::reply_done(None, ctx.clone()));
                                return None;
                            } else if data.position_final > guard.position_current {
                                // move to left
                                guard.set_direction(uln2003::Direction::Normal);
                            } else {
                                // move to right
                                guard.set_direction(uln2003::Direction::Reverse);
                            }
                            let mut position_abs_diff =
                                guard.position_current.abs_diff(data.position_final);
                            drop(guard);

                            let (tx_done, rx_done) = flume::bounded::<bool>(1);

                            let timer = unsafe {
                                let stepper1 = std::sync::Arc::clone(&stepper1);
                                let mut done = false;
                                timer_service
                                    .timer_nonstatic(move || {
                                        if !done {
                                            let mut guard = stepper1.lock().unwrap();
                                            guard.step();

                                            let position_abs_diff_next = guard
                                                .position_current
                                                .abs_diff(data.position_final);

                                            if position_abs_diff_next==0 {
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

                            let mut done_a = false;
                            let mut done_b = false;
                            let mut result: Option<(event::Action, event::Context)> = None;

                            while !done_a && !done_b {
                                flume::Selector::new()
                                    .recv(&rx_done, |success| {
                                        let success = success.unwrap_or(false);
                                        if success {
                                            tx_eventer_from_worker
                                                .send(event::reply_done(None, ctx.clone()));
                                        } else {
                                            tx_eventer_from_worker.send(event::reply_error(
                                                String::from("failed; guess logical error; move left right position_current and final etc, gets confusing. This needs a fix!!"),
                                                ctx.clone(),
                                            ));
                                        }
                                        done_a = true;
                                    })
                                    .recv(&rx_worker, |e| {
                                        let (action, ctx_next) = e.unwrap();
                                        match action {
                                            event::Action::ActionGet((action_get)) => {
                                                // while action_run is active, answer to simple action_get commands
                                                fn_handle_get(action_get, ctx_next);
                                            }
                                            _ => {
                                                // got action_set/run, this terminates the active action_run command
                                                result = Some((action, ctx_next));
                                                done_b = true;
                                                tx_eventer_from_worker.send(event::Event::ActionReply((
                                                    event::ActionReply::Cancel(),
                                                    ctx.clone(),
                                                )));
                                            }
                                        }
                                    })
                                    .wait();
                            }

                            timer.cancel();
                            drop(timer);

                            result
                        };

                    let fn_stepper1_speed =
                        |data: &event::DataReqRunStepper1Speed,
                         ctx: event::Context,
                         dir: uln2003::Direction|
                         -> Option<(event::Action, event::Context)> {
                            tx_eventer_from_worker.send(event::reply_ack(None, ctx.clone()));

                            let step_delay = std::time::Duration::from_micros(data.speed);
                            let mut stepper_guard = stepper1.lock().unwrap();
                            stepper_guard.set_direction(dir);
                            drop(stepper_guard);

                            let timer = unsafe {
                                let stepper1 = std::sync::Arc::clone(&stepper1);
                                timer_service
                                    .timer_nonstatic(move || {
                                        let mut stepper_guard = stepper1.lock().unwrap();
                                        stepper_guard.step();
                                        drop(stepper_guard);
                                    })
                                    .unwrap()
                            };

                            timer.every(step_delay).unwrap();

                            let mut result: Option<(event::Action, event::Context)> = None;

                            loop {
                                let (action, ctx) = rx_worker.recv().unwrap();
                                match action {
                                    event::Action::ActionGet((action_get)) => {
                                        // while action_run is active, answer to simple action_get commands
                                        fn_handle_get(action_get, ctx);
                                        continue;
                                    }
                                    _ => {
                                        // got action_set/run, this terminates the active action_run command
                                        result = Some((action, ctx));
                                        break;
                                    }
                                }
                            }

                            timer.cancel();
                            drop(timer);

                            let mut stepper_guard = stepper1.lock().unwrap();
                            stepper_guard.stop();
                            drop(stepper_guard);

                            tx_eventer_from_worker.send(event::Event::ActionReply((
                                event::ActionReply::Cancel(),
                                ctx.clone(),
                            )));

                            result
                        };

                    'loop_recv: loop {
                        let mut recv = rx_worker.recv().unwrap();
                        'loop_match: loop {
                            let action = recv.0;
                            let ctx = recv.1;
                            match action {
                                event::Action::ActionGet((action_get)) => {
                                    fn_handle_get(action_get, ctx);
                                    continue 'loop_recv;
                                }
                                event::Action::ActionRun((action_run)) => match action_run {
                                    event::ActionRun::P2PInit { ref data } => {
                                        tx_eventer_from_worker.send(event::reply_ack(None, ctx.clone()));
                                        let mut guard= p2p_token.lock().unwrap();
                                        *guard=data.p2p_token;
                                        drop(guard);
                                        
                                        tx_eventer_from_worker.send(event::reply_done(None, ctx.clone()));
                                        continue 'loop_recv;
                                    }
                                    event::ActionRun::Stop() => continue 'loop_recv,
                                    event::ActionRun::Stepper1MoveTo { ref data } => {
                                        match fn_stepper1_move_to(
                                            data,
                                            &ctx,
                                        ) {
                                            Some(recv_new) => {
                                                recv = recv_new;
                                                continue 'loop_match;
                                            }
                                            None => continue 'loop_recv,
                                        }
                                    }
                                    event::ActionRun::Stepper1SpeedLeft { ref data } => {
                                        match fn_stepper1_speed(
                                            data,
                                            ctx.clone(),
                                            uln2003::Direction::Normal,
                                        ) {
                                            Some(recv_new) => {
                                                recv = recv_new;
                                                continue 'loop_match;
                                            }
                                            None => continue 'loop_recv,
                                        }
                                    }
                                    event::ActionRun::Stepper1SpeedRight { ref data } => {
                                        match fn_stepper1_speed(
                                            data,
                                            ctx.clone(),
                                            uln2003::Direction::Reverse,
                                        ) {
                                            Some(recv_new) => {
                                                recv = recv_new;
                                                continue 'loop_match;
                                            }
                                            None => continue 'loop_recv,
                                        }
                                    }
                                    event::ActionRun::UpdateBoard { data } => {
                                        tx_eventer_from_worker
                                            .send(event::reply_ack(None, ctx.clone()));

                                        match simple_download_and_update_firmware(data.url) {
                                            Ok(_) => {
                                                tx_eventer_from_worker
                                                    .send(event::reply_done(None, ctx.clone()));

                                                //wait done message is out
                                                std::thread::sleep(std::time::Duration::from_secs(
                                                    3,
                                                ));
                                                unsafe {
                                                    esp_idf_svc::sys::esp_restart();
                                                }
                                            }
                                            Err(err) => {
                                                info!("worker update_board err: {:?}", err);
                                                tx_eventer_from_worker
                                                    .send(event::reply_error(err.to_string(), ctx));
                                            }
                                        }
                                        continue 'loop_recv;
                                    }
                                    _ => continue 'loop_recv,
                                },
                                _ => continue 'loop_recv,
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
    let (mqtt_client, mqtt_conn) = EspMqttClient::new(
        url,
        &MqttClientConfiguration {
            client_id: Some(client_id),
            disable_clean_session: true,

            ..Default::default()
        },
    )?;

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

fn get_firmware_info(buff: &[u8]) -> Result<FirmwareInfo, EspError> {
    let mut loader = EspFirmwareInfoLoader::new();
    loader.load(buff)?;
    loader.get_info()
}
#[macro_export]
macro_rules! esp_err {
    ($x:ident) => {
        EspError::from_infallible::<$x>()
    };
}

pub fn simple_download_and_update_firmware(url: String) -> Result<(), EspError> {
    let mut client = Client::wrap(EspHttpConnection::new(
        &esp_idf_svc::http::client::Configuration {
            buffer_size: Some(1024 * 4),
            ..Default::default()
        },
    )?);

    let headers = [("accept", "application/octet-stream")];

    //let headers = [(ACCEPT.as_str(), mime::APPLICATION_OCTET_STREAM.as_ref())];
    let surl = url.to_string();
    let request = client
        .request(Method::Get, &surl, &headers)
        .map_err(|e| e.0)?;

    let mut response = request.submit().map_err(|e| e.0)?;
    if response.status() != 200 {
        log::info!("Bad HTTP response: {}", response.status());
        return Err(esp_err!(ESP_ERR_INVALID_RESPONSE));
    }

    let file_size = response.content_len().unwrap_or(0) as usize;
    if file_size <= FIRMWARE_MIN_SIZE {
        log::info!(
            "File size is {file_size}, too small to be a firmware! No need to proceed further."
        );
        return Err(esp_err!(ESP_ERR_IMAGE_INVALID));
    }
    if file_size > FIRMWARE_MAX_SIZE {
        log::info!("File is too big ({file_size} bytes).");
        return Err(esp_err!(ESP_ERR_IMAGE_INVALID));
    }

    let mut ota = EspOta::new()?;
    let mut work = ota.initiate_update()?;
    let mut buff = vec![0; FIRMWARE_DOWNLOAD_CHUNK_SIZE];
    let mut total_read_len: usize = 0;
    let mut got_info = false;

    let dl_result = loop {
        let n = response.read(&mut buff).unwrap_or_default();
        total_read_len += n;
        if !got_info {
            match get_firmware_info(&buff[..n]) {
                Ok(info) => log::info!("Firmware to be downloaded: {info:?}"),
                Err(e) => {
                    log::error!("Failed to get firmware info from downloaded bytes!");
                    break Err(e);
                }
            };
            got_info = true;
        }
        if n > 0 {
            if let Err(e) = work.write(&buff[..n]) {
                log::error!("Failed to write to OTA. {e}");
                break Err(e);
            }
        }
        if total_read_len >= file_size {
            break Ok(());
        }
    };
    if dl_result.is_err() {
        return work.abort();
    }
    if total_read_len < file_size {
        log::error!("Supposed to download {file_size} bytes, but we could only get {total_read_len}. May be network error?");
        return work.abort();
    }
    work.complete()
}
