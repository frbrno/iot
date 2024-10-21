# rpc

A nats.Conn wrapper providing request-reply support for MQTT nodes, with nats.io JetStream-based message persistence.

#### protocol scheme

nats subject layout:  
`iot.{dst}.{direction rx/tx}.{src}.{method}.{status}.{resource}.{token}`

method get/set:  
client sends-->`iot.rusty_falcon.rx.goofy_hawk.[get,set].exec.info.1234`  
node answer->`iot.rusty_falcon.tx.goofy_hawk.[get,set].[ack,error].info.1234`  

method run:  
client sends----->`iot.rusty_falcon.rx.goofy_hawk.run.exec.update_board.1456`  
node answer 1->`iot.rusty_falcon.tx.goofy_hawk.run.[ack,error].update_board.1456`  
node answer 2->`iot.rusty_falcon.tx.goofy_hawk.run.[done,error,cancel].update_board.1456`  

*rusty_falcon is the node, goofy_hawk the client

The node can only process one 'run' method at a time.<br>
If a new 'run' method is received while another is active,<br>
the node will send a cancel message for the current operation before starting the new 'run' method.

example:  
client sends->`iot.rusty_falcon.rx.goofy_hawk.run.exec.stepper1_move_to.1000`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.ack.stepper1_move_to.1000`  
client sends->`iot.rusty_falcon.rx.goofy_hawk.run.exec.stepper1_move_to.1001`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.cancel.stepper1_move_to.1000`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.ack.stepper1_move_to.1001`  
node sends-->`iot.rusty_falcon.tx.goofy_hawk.run.done.stepper1_move_to.1001`  











