use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};

pub fn action_from_mqtt(topic: &str, data: &[u8]) -> Result<(Action, Context)> {
    let parts: Vec<&str> = topic.split('/').collect();
    if parts.len() != 5 {
        return Err(anyhow!("unknown topic {:?}", topic));
    }
    let ctx = Context {
        src: parts[0].to_string(),
        dst: parts[1].to_string(),
        cmd: parts[2].to_string(),
        action: parts[3].to_string(),
        token: parts[4].to_string(),
    };

    match (ctx.action.as_str(), ctx.cmd.as_str()) {
        ("get", "stepper1_state") => Ok((Action::ActionGet(ActionGet::Stepper1State()), ctx)),
        ("run", "stepper1_speed") => Ok((
            Action::ActionRun(ActionRun::Stepper1Speed {
                data: serde_json::from_slice(data)?,
            }),
            ctx,
        )),
        ("run", "stepper1_speed_left") => Ok((
            Action::ActionRun(ActionRun::Stepper1SpeedLeft {
                data: serde_json::from_slice(data)?,
            }),
            ctx,
        )),
        ("run", "stepper1_speed_right") => Ok((
            Action::ActionRun(ActionRun::Stepper1SpeedRight {
                data: serde_json::from_slice(data)?,
            }),
            ctx,
        )),
        ("run", "stop") => Ok((Action::ActionRun(ActionRun::Stop()), ctx)),
        ("run", "update_board") => Ok((
            Action::ActionRun(ActionRun::UpdateBoard {
                data: serde_json::from_slice(data)?,
            }),
            ctx,
        )),
        _ => Err(anyhow!(
            "unknown request. action: {:?}, cmd: {:?}",
            ctx.action,
            ctx.cmd
        )),
    }
}

#[derive(Clone)]
pub struct Context {
    pub src: String,
    pub dst: String,
    pub cmd: String,
    pub token: String,
    pub action: String,
}
pub enum Event {
    ActionReply((ActionReply, Context)),
    Action((Action, Context)),
}

pub fn reply_ack(data: Option<ReplyData>, ctx: Context) -> Event {
    Event::ActionReply((ActionReply::Ack { data: data }, ctx))
}

pub fn reply_done(data: Option<ReplyData>, ctx: Context) -> Event {
    Event::ActionReply((ActionReply::Done { data: data }, ctx))
}

pub fn reply_error(msg: String, ctx: Context) -> Event {
    Event::ActionReply((
        ActionReply::Error {
            data: Some(ReplyData::WithString { message: msg }),
        },
        ctx,
    ))
}

pub enum Action {
    ActionRun(ActionRun),
    ActionGet(ActionGet),
}

pub enum ActionRun {
    Stepper1Speed { data: DataReqRunStepper1Speed },
    Stepper1SpeedLeft { data: DataReqRunStepper1Speed },
    Stepper1SpeedRight { data: DataReqRunStepper1Speed },
    Stepper1SetHomePosition(),
    Stop(),
    UpdateBoard { data: DataReqRunUpdateBoard },
}

pub enum ActionGet {
    Stepper1State(),
}

#[derive(Serialize, Deserialize)]
#[serde(untagged)]
pub enum ActionReply {
    Ack { data: Option<ReplyData> },
    Done { data: Option<ReplyData> },
    Cancel(),
    Error { data: Option<ReplyData> },
}

impl ActionReply {
    pub fn data(&self) -> &Option<ReplyData> {
        match self {
            ActionReply::Ack { data } => data,
            ActionReply::Done { data } => data,
            ActionReply::Error { data } => data,
            ActionReply::Cancel() => &None,
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
        current_position: usize,
        direction: String,
    },
}

impl ActionReply {
    pub fn topic(&self, ctx: &Context) -> String {
        match self {
            ActionReply::Ack { .. } => {
                format!("{}/{}/{}/ack/{}", ctx.dst, ctx.src, ctx.cmd, ctx.token)
            }
            ActionReply::Done { .. } => {
                format!("{}/{}/{}/done/{}", ctx.dst, ctx.src, ctx.cmd, ctx.token)
            }
            ActionReply::Cancel() => {
                format!("{}/{}/{}/cancel/{}", ctx.dst, ctx.src, ctx.cmd, ctx.token)
            }
            ActionReply::Error { .. } => {
                format!("{}/{}/{}/error/{}", ctx.dst, ctx.src, ctx.cmd, ctx.token)
            }
        }
    }
}

#[derive(Serialize, Deserialize)]
pub struct DataReqRunStepper1Speed {
    pub speed: u64,
}

#[derive(Serialize, Deserialize)]
pub struct DataReqRunUpdateBoard {
    pub url: String,
}

#[derive(Serialize, Deserialize)]
pub struct ReplyStepper1State {
    pub current_position: usize,
    pub direction: String,
}
