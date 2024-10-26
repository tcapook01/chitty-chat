package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	gRPC "github.com/tcapook01/chitty-chat/proto"
	"google.golang.org/grpc"
)

type chatServer struct {
	gRPC.UnimplementedChittyChatServer // Required for gRPC server
	name                               string
	port                               string

	mutex        sync.Mutex                                     // To avoid race conditions
	lamportTime  int64                                          // Lamport timestamp
	clients      map[string]gRPC.ChittyChat_MessageStreamServer // Store connected clients
	clientIDs    map[string]int
	nextClientID int // Next available client ID
}

// flags for terminal parameters
var serverName = flag.String("name", "default", "Server name")
var port = flag.String("port", "5400", "Server port")

func main() {
	f := setLog() // Set up logging to a file
	defer f.Close()

	// Parse command-line flags
	flag.Parse()
	fmt.Println(".:Chitty-Chat server starting:.")
	log.Println(".:Chitty-Chat server starting:.")

	// Channels to receive server instance and errors
	serverChan := make(chan *grpc.Server)
	done := make(chan error)

	// Start the server in goroutine
	go launchServer(serverChan, done) // Start the server

	// Retrieve the server instance
	grpcServer := <-serverChan

	// Channel to listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Block iuntil a signal is received
	sig := <-sigChan
	fmt.Printf("Received signal %s, initiating graceful shutdown...\n", sig)
	log.Printf("Received signal %s, initiating graceful shutdown...\n", sig)

	// Initiate graceful shutdown
	log.Println("Initiating GracefulStop()")
	grpcServer.GracefulStop()
	log.Println("GracefulStop() completed")

	// Wait for the server goroutine to signal completion
	err := <-done
	if err != nil {
		log.Fatalf("Server encountered an error during shutdown: %v", err)
	}

	// Print confirmation after shutdown is compelte
	fmt.Println("Graceful shutdown complete. Server stopped")
	log.Println("Graceful shutdown complete. Server Stopped")
}

func launchServer(serverChan chan<- *grpc.Server, done chan<- error) {
	fmt.Printf("Server %s attempting to listen on port %s\n", *serverName, *port)
	log.Printf("Server %s attempting to listen on port %s\n", *serverName, *port)

	// Create a TCP listener on the specified port
	list, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", *port))
	if err != nil {
		log.Printf("Error listening on port %s: %v", *port, err)
		done <- err
		return
	}

	// Create gRPC server
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)

	// Create a new instance of chatServer
	server := &chatServer{
		name:         *serverName,
		port:         *port,
		clients:      make(map[string]gRPC.ChittyChat_MessageStreamServer),
		clientIDs:    make(map[string]int),
		nextClientID: 1,
		lamportTime:  0,
	}

	// Register the chat server with the gRPC server
	gRPC.RegisterChittyChatServer(grpcServer, server)

	// Inform main that the server is ready
	serverChan <- grpcServer

	fmt.Printf("Server %s listening at %v\n", *serverName, list.Addr())
	log.Printf("Server %s listening at %v\n", *serverName, list.Addr())

	// Start serving
	if err := grpcServer.Serve(list); err != nil {
		log.Printf("Server stopped with error: %v", err)
		done <- err
		return
	}
	// Indicate that server has stopped without error
	log.Println("Server shutdown succesfully")
	done <- nil
}

// Handles message stream in the chat
func (s *chatServer) MessageStream(stream gRPC.ChittyChat_MessageStreamServer) error {
	var participantName string
	hasLeft := false // Departure flag

	for {
		// Receive the next message from the stream
		msg, err := stream.Recv()
		if err == io.EOF {
			if participantName != "" && !hasLeft {
				s.handleLeave(participantName, true)
				hasLeft = true
			}
			break // Stream has ended
		}
		if err != nil {
			if participantName != "" && !hasLeft {
				s.handleLeave(participantName, true)
				hasLeft = true
			}
			log.Printf("Error receiving message from %s: %v", participantName, err)
			return err
		}

		// Enforce that the first message must be 'JOIN'
		if participantName == "" {
			if msg.Payload == nil {
				errorMsg := fmt.Sprintf("Participant attempted to send empty payload before joining.")
				fmt.Println(errorMsg)
				log.Println(errorMsg)

				// Send acknowledgement back to the client
				ack := &gRPC.ServerMessage{
					Payload: &gRPC.ServerMessage_Ack{
						Ack: &gRPC.Ack{
							Success: false,
							Info:    "You must join the chat before sending messages.",
						},
					},
				}
				if err := stream.Send(ack); err != nil {
					log.Printf("Error sending acknowlegment: %v", err)
				}
				continue
			}

			// Type assertion to determine the payload type.
			switch payload := msg.Payload.(type) {
			case *gRPC.ClientMessage_Join:
				participantName = payload.Join.ParticipantName

				// Protect shared resources
				s.mutex.Lock()
				if _, exists := s.clients[participantName]; exists {
					s.mutex.Unlock()
					errorMsg := fmt.Sprintf("Participant %s is already joined.", participantName)
					fmt.Println(errorMsg)
					log.Println(errorMsg)

					// Send acknowledgment back to the client
					ack := &gRPC.ServerMessage{
						Payload: &gRPC.ServerMessage_Ack{
							Ack: &gRPC.Ack{
								Success: false,
								Info:    "You have already joined the chat.",
							},
						},
					}
					if err := stream.Send(ack); err != nil {
						log.Printf("Error sending acknowledgment: %v", err)
					}
					return nil // Terminate the stream as it's an invalid state
				}

				s.clients[participantName] = stream
				s.clientIDs[participantName] = s.nextClientID
				s.nextClientID++
				s.mutex.Unlock()

				joinMsg := fmt.Sprintf("Participant %s joined Chitty-Chat at Lamport time %d", participantName, s.lamportTime)
				fmt.Println(joinMsg)
				log.Println(joinMsg)

				// Create Broadcast message
				broadcastMsg := &gRPC.ServerMessage{
					Payload: &gRPC.ServerMessage_Broadcast{
						Broadcast: &gRPC.BroadcastMessage{
							ParticipantName: participantName,
							Message:         joinMsg,
							LamportTime:     s.lamportTime,
						},
					},
				}
				s.broadcast(broadcastMsg)
				continue

			default:
				errorMsg := fmt.Sprintf("Participant %s attempted to send unknown payload before joining.", participantName)
				fmt.Println(errorMsg)
				log.Println(errorMsg)

				// Send acknowledgement back to the client
				ack := &gRPC.ServerMessage{
					Payload: &gRPC.ServerMessage_Ack{
						Ack: &gRPC.Ack{
							Success: false,
							Info:    "You must join the chat before sending messages",
						},
					},
				}
				if err := stream.Send(ack); err != nil {
					log.Printf("Error sending acknowledgement: %v", err)
				}
				continue
			}
		}

		// After joining, handle other message types
		if msg.Payload == nil {
			errorMsg := fmt.Sprintf("Participant %s sent empty payload.", participantName)
			fmt.Println(errorMsg)
			log.Println(errorMsg)

			// Send acknowlegdement back to client
			ack := &gRPC.ServerMessage{
				Payload: &gRPC.ServerMessage_Ack{
					Ack: &gRPC.Ack{
						Success: false,
						Info:    "Empty message received.",
					},
				},
			}
			if err := stream.Send(ack); err != nil {
				log.Printf("Error sending acknowlegdment: %v", err)
			}
			continue
		}

		// Type assertion to determine the payload type
		switch payload := msg.Payload.(type) {
		case *gRPC.ClientMessage_Publish:
			// Handle PublishMessage

			// Access lampoer time from Pubilsh request
			clientLamportTime := payload.Publish.LamportTime

			// Update server´s lamport timestamp
			s.mutex.Lock()
			if clientLamportTime > s.lamportTime {
				s.lamportTime = clientLamportTime + 1
			} else {
				s.lamportTime++
			}
			s.mutex.Unlock()

			if len(payload.Publish.Message) > 128 {
				errorMsg := fmt.Sprintf("Participant %s sent a message exceeding 128 characters.", participantName)
				fmt.Println(errorMsg)
				log.Println(errorMsg)

				// Send acknowlegdement back to the client
				ack := &gRPC.ServerMessage{
					Payload: &gRPC.ServerMessage_Ack{
						Ack: &gRPC.Ack{
							Success: false,
							Info:    "Message exceeds 128 characters and was not sent.",
						},
					},
				}
				if err := stream.Send(ack); err != nil {
					log.Printf("Error sending acknowledgment to %s: %v", participantName, err)
				}
				continue
			}

			// Create broadcast message
			chatMsg := fmt.Sprintf("Message from %s: %s at Lamport time %d", participantName, payload.Publish.Message, s.lamportTime)
			fmt.Println(chatMsg)
			log.Println(chatMsg)

			broadcastMsg := &gRPC.ServerMessage{
				Payload: &gRPC.ServerMessage_Broadcast{
					Broadcast: &gRPC.BroadcastMessage{
						ParticipantName: participantName,
						Message:         chatMsg,
						LamportTime:     s.lamportTime,
					},
				},
			}
			s.broadcast(broadcastMsg)

		case *gRPC.ClientMessage_Leave:
			// Handle LeaveRequest
			s.handleLeave(participantName, true)
			hasLeft = true
			return nil // Terminate the stream after handling leave

		default:
			errorMsg := fmt.Sprintf("Participant %s sent unknown payload.", participantName)
			fmt.Println(errorMsg)
			log.Println(errorMsg)

			// Send acknowledgment back to the client
			ack := &gRPC.ServerMessage{
				Payload: &gRPC.ServerMessage_Ack{
					Ack: &gRPC.Ack{
						Success: false,
						Info:    "Unknown message type.",
					},
				},
			}
			if err := stream.Send(ack); err != nil {
				log.Printf("Error sending acknowledgment: %v", err)
			}
			continue
		}
	}
	return nil
}

func (s *chatServer) handleLeave(participantName string, broadcast bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check if the participant is still in the clients map
	if _, exists := s.clients[participantName]; !exists {
		// Participant already left; no action needed
		return
	}

	// Remove the participant from the clients map
	delete(s.clients, participantName)
	delete(s.clientIDs, participantName)

	leaveMsg := fmt.Sprintf("Participant %s left Chitty-Chat at Lamport time %d", participantName, s.lamportTime)
	fmt.Println(leaveMsg)
	log.Println(leaveMsg)

	if broadcast {
		// Create Broadcast message
		broadcastMsg := &gRPC.ServerMessage{
			Payload: &gRPC.ServerMessage_Broadcast{
				Broadcast: &gRPC.BroadcastMessage{
					ParticipantName: participantName,
					Message:         leaveMsg,
					LamportTime:     s.lamportTime,
				},
			},
		}
		s.broadcast(broadcastMsg)
	}
}

// Send messages to all connected clients
func (s *chatServer) broadcast(msg *gRPC.ServerMessage) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for name, stream := range s.clients {
		go func(name string, stream gRPC.ChittyChat_MessageStreamServer) {
			if err := stream.Send(msg); err != nil {
				log.Printf("Error sending message to %s: %v", name, err)
			}
		}(name, stream)
		// Removed so the sender can see its own messages.
		/*if name != msg.ParticipantName {
			if err := stream.Send(msg); err != nil {
				log.Printf("Error sending message to %s: %v", name, err)
			}
		}*/
	}
}

// Set up logger to write to a log file
func setLog() *os.File {
	logFileName := "log_server.txt"

	// Truncate the log file at the start
	if err := os.Truncate(logFileName, 0); err != nil {
		fmt.Printf("Error truncating log file: %v\n", err)
		log.Fatalf("Error truncating log file: %v", err)
	}

	f, err := os.OpenFile("log_server.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}

	// Set log output and format
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return f
}
