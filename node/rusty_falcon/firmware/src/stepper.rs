use uln2003::{
	StepperMotor,
	ULN2003,
};

pub struct Stepper {
	ext: Box<dyn uln2003::StepperMotor + Send>,
	pub position_current: usize,
	pub direction: uln2003::Direction,
}

impl Stepper {
	pub fn new(motor: Box<dyn uln2003::StepperMotor + Send>) -> Self {
		Stepper {
			ext: motor,
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
