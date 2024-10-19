use anyhow::{
	Result,
	anyhow,
};
use serde::{
	Deserialize,
	Serialize,
};

// just cmds right now
pub fn from_mqtt(topic: &str, data: &[u8]) -> Result<(Cmd, Context)> {
	let parts: Vec<&str> = topic.split('/').collect();
	if parts.len() != 5 {
		return Err(anyhow!("unknown topic {:?}", topic));
	}
	let ctx = Context {
		src: parts[0].to_string(),
		dst: parts[1].to_string(),
		event: parts[2].to_string(),
		event_typ: parts[3].to_string(),
		token: parts[4].to_string(),
	};

	// println!(
	// 	"src: {:?}, dst: {:?}, event: {:?}, event_typ: {:?}, token: {:?},",
	// 	ctx.src, ctx.dst, ctx.event, ctx.event_typ, ctx.token
	// );

	match (ctx.event_typ.as_str(), ctx.event.as_str()) {
		("get", "stepper1_state") => Ok((Cmd::CmdGet(CmdGet::Stepper1State()), ctx)),
		("get", "p2p_token") => Ok((Cmd::CmdGet(CmdGet::P2PToken()), ctx)),
		("get", "info") => Ok((Cmd::CmdGet(CmdGet::Info()), ctx)),
		("run", "p2p_init") => Ok((
			Cmd::CmdRun(CmdRun::P2PInit {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		("run", "stepper1_move_to") => Ok((
			Cmd::CmdRun(CmdRun::Stepper1MoveTo {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		("run", "stepper1_speed") => Ok((
			Cmd::CmdRun(CmdRun::Stepper1Speed {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		("run", "stepper1_speed_left") => Ok((
			Cmd::CmdRun(CmdRun::Stepper1SpeedLeft {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		("run", "stepper1_speed_right") => Ok((
			Cmd::CmdRun(CmdRun::Stepper1SpeedRight {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		("run", "stop") => Ok((Cmd::CmdRun(CmdRun::Stop()), ctx)),
		("run", "update_board") => Ok((
			Cmd::CmdRun(CmdRun::UpdateBoard {
				data: serde_json::from_slice(data)?,
			}),
			ctx,
		)),
		_ => Err(anyhow!(
			"unknown command. event: {:?}, event_typ: {:?}",
			ctx.event,
			ctx.event_typ
		)),
	}
}

#[derive(Clone)]
pub struct Context {
	pub src: String,
	pub dst: String,
	pub event: String,
	pub event_typ: String,
	pub token: String,
}
pub enum Event {
	CmdReply((CmdReply, Context)),
	Cmd((Cmd, Context)),
}

pub fn reply_ack(data: Option<ReplyData>, ctx: &Context) -> Event {
	Event::CmdReply((CmdReply::Ack { data }, ctx.clone()))
}

pub fn reply_done(data: Option<ReplyData>, ctx: &Context) -> Event {
	Event::CmdReply((CmdReply::Done { data }, ctx.clone()))
}

pub fn reply_cancel(data: Option<ReplyData>, ctx: &Context) -> Event {
	Event::CmdReply((CmdReply::Cancel(), ctx.clone()))
}

pub fn reply_error(msg: String, ctx: &Context) -> Event {
	Event::CmdReply((
		CmdReply::Error {
			data: Some(ReplyData::WithString { message: msg }),
		},
		ctx.clone(),
	))
}

pub enum Cmd {
	CmdRun(CmdRun),
	CmdGet(CmdGet),
}

pub enum CmdRun {
	Stepper1Speed { data: DataReqRunStepper1Speed },
	Stepper1SpeedLeft { data: DataReqRunStepper1Speed },
	Stepper1SpeedRight { data: DataReqRunStepper1Speed },
	Stepper1SetHomePosition(),
	Stepper1MoveTo { data: RequestStepper1MoveTo },
	Stop(),
	UpdateBoard { data: DataReqRunUpdateBoard },
	P2PInit { data: RequestP2PInit },
}

pub enum CmdGet {
	Stepper1State(),
	P2PToken(),
	Info(),
}

#[derive(Serialize, Deserialize)]
#[serde(untagged)]
pub enum CmdReply {
	Ack { data: Option<ReplyData> },
	Done { data: Option<ReplyData> },
	Error { data: Option<ReplyData> },
	Cancel(),
}

impl CmdReply {
	pub fn data(&self) -> &Option<ReplyData> {
		match self {
			CmdReply::Ack { data } => data,
			CmdReply::Done { data } => data,
			CmdReply::Error { data } => data,
			CmdReply::Cancel() => &None,
		}
	}
	pub fn topic(&self, ctx: &Context) -> String {
		match self {
			CmdReply::Ack { .. } => {
				format!("{}/{}/{}/ack/{}", ctx.dst, ctx.src, ctx.event, ctx.token)
			}
			CmdReply::Done { .. } => {
				format!("{}/{}/{}/done/{}", ctx.dst, ctx.src, ctx.event, ctx.token)
			}
			CmdReply::Cancel() => {
				format!("{}/{}/{}/cancel/{}", ctx.dst, ctx.src, ctx.event, ctx.token)
			}
			CmdReply::Error { .. } => {
				format!("{}/{}/{}/error/{}", ctx.dst, ctx.src, ctx.event, ctx.token)
			}
		}
	}
}

#[derive(Serialize, Deserialize)]
#[serde(untagged)]
pub enum ReplyData {
	WithString {
		message: String,
	},
	Stepper1State {
		position_current: usize,
		direction: String,
	},
	P2PToken {
		p2p_token: i64,
	},
	Info {
		build_time: String,
	},
}

#[derive(Serialize, Deserialize)]
pub struct DataReqRunStepper1Speed {
	pub speed: u64,
}

#[derive(Serialize, Deserialize)]
pub struct RequestStepper1MoveTo {
	pub position_final: usize,
	pub step_delay_micros: usize,
}

#[derive(Serialize, Deserialize)]
pub struct RequestP2PInit {
	pub p2p_token: i64,
}

#[derive(Serialize, Deserialize)]
pub struct DataReqRunUpdateBoard {
	pub url: String,
}

#[derive(Serialize, Deserialize)]
pub struct ReplyStepper1State {
	pub position_current: usize,
	pub direction: String,
}
