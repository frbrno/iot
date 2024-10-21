# rpc

nats.Conn wrapper for request reply support for mqtt nodes and jetstream message persistent

#### protocol scheme

message layout:  
`iot.{dst}.{direction rx/tx}.{src}.{method}.{status}.{resource}.{token}`

method get/set:  
client sends-->`iot.rusty_falcon.rx.goofy_hawk.[get,set].exec.info.1234`  
node answer->`iot.rusty_falcon.tx.goofy_hawk.[get,set].[ack,error].info.1234`  

method run:  
client sends----->`iot.rusty_falcon.rx.goofy_hawk.run.exec.update_board.1456`  
node answer 1->`iot.rusty_falcon.tx.goofy_hawk.run.[ack,error].update_board.1456`  
node answer 2->`iot.rusty_falcon.tx.goofy_hawk.run.[done,error,cancel].update_board.1456`  

*rusty_falcon is the node, goofy_hawk the client

The node is only able to process one 'run' method at a time.  
Sending a new 'run' method while one is active, 
results in the node sending a cancel message and starts the new 'run' method.

example:  
client sends->`iot.rusty_falcon.rx.goofy_hawk.run.exec.stepper1_move_to.1000`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.ack.stepper1_move_to.1000`  
client sends->`iot.rusty_falcon.rx.goofy_hawk.run.exec.stepper1_move_to.1001`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.cancel.stepper1_move_to.1000`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.ack.stepper1_move_to.1001`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.done.stepper1_move_to.1001`  











