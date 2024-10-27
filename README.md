# chittychat
Not sure that this is the right way but here goes (the demonstrations: Technical Requirements number 5, 6, and 7) ... 

Demonstrate that the system can be started with at least 3 client nodes: 

How to run the Chitty-Chat system:

```
cd server
```

```
go build server.go
```

```
./server --name=Server1 --port=5400
```



New terminal (One for the server and one for each of the clients)

Do this three times with different names (example: Sofia, Tomas, and Vicki):

```
cd client
```

```
go build client.go
```

```
./client --name=Sofia --server=localhost:5400
```

Server terminal:
Participant Sofia joined Chitty-Chat at Lamport time 0
Participant Tomas joined Chitty-Chat at Lamport time 0
Participant Vicki joined Chitty-Chat at Lamport time 0


The commands above also demonstrates that a client node can join the system. 

Demonstrate that a client node can leave the system:

- For a client to leave, the client has to write "EXIT"  
The message "Exiting chat. Goodbye!" will then come up

On the server:
Participant Sofia left Chitty-Chat at Lamport time 2
