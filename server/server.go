package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

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

	launchServer() // Start the server
}

func launchServer() {
	fmt.Printf("Server %s attempting to listen on port %s\n", *serverName, *port)
	log.Printf("Server %s attempting to listen on port %s\n", *serverName, *port)

	// Create a TCP listener on the specified port
	list, err := net.Listen("tcp", fmt.Sprintf("localhost:%s", *port))
	if err != nil {
		log.Fatalf("Error listening on port %s: %v", *port, err)
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

	fmt.Printf("Server %s listening at %v\n", *serverName, list.Addr())
	log.Printf("Server %s listening at %v\n", *serverName, list.Addr())

	if err := grpcServer.Serve(list); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// Handles message stream in the chat
func (s *chatServer) MessageStream(stream gRPC.ChittyChat_MessageStreamServer) error {
	for {
		// Receive the next message from the stream
		msg, err := stream.Recv()
		if err == io.EOF {
			break // Stream has ended
		}
		if err != nil {
			return err
		}

		s.mutex.Lock()  // Lock to prevent race conditions
		s.lamportTime++ // Increment the Lamport timestamp
		s.mutex.Unlock()

		// If the message indicates a client joining
		if msg.Message == "JOIN" {
			s.clients[msg.ParticipantName] = stream
			s.clientIDs[msg.ParticipantName] = s.nextClientID
			s.nextClientID++

			joinMsg := fmt.Sprintf("Participant %s joined Chitty-Chat at Lamport time %d", msg.ParticipantName, s.lamportTime)
			fmt.Println(joinMsg)
			log.Println(joinMsg)

			s.broadcast(&gRPC.BroadcastMessage{
				ParticipantName: msg.ParticipantName,
				Message:         joinMsg,
				LamportTime:     s.lamportTime,
			})
		}

		// If the message indicates a client leaving
		if msg.Message == "LEAVE" {
			leaveMsg := fmt.Sprintf("Participant %s left Chitty-Chat at Lamport time %d", msg.ParticipantName, s.lamportTime)
			fmt.Println(leaveMsg)
			log.Println(leaveMsg)

			s.broadcast(&gRPC.BroadcastMessage{
				ParticipantName: msg.ParticipantName,
				Message:         leaveMsg,
				LamportTime:     s.lamportTime,
			})

			delete(s.clients, msg.ParticipantName) // Remove client from the list
			delete(s.clientIDs, msg.ParticipantName)
		}

		// For regular chat messages
		if msg.Message != "JOIN" && msg.Message != "LEAVE" {
			chatMsg := fmt.Sprintf("Message from %s: %s at Lamport time %d", msg.ParticipantName, msg.Message, s.lamportTime)
			fmt.Println(chatMsg)
			log.Println(chatMsg)

			s.broadcast(&gRPC.BroadcastMessage{
				ParticipantName: msg.ParticipantName,
				Message:         chatMsg,
				LamportTime:     s.lamportTime,
			})
		}
	}

	return nil
}

// Send messages to all clients except the sender
func (s *chatServer) broadcast(msg *gRPC.BroadcastMessage) {
	for name, stream := range s.clients {
		if name != msg.ParticipantName {
			if err := stream.Send(msg); err != nil {
				log.Printf("Error sending message to %s: %v", name, err)
			}
		}
	}
}

// Set up logger to write to a log file
func setLog() *os.File {
	if err := os.Truncate("log_server.txt", 0); err != nil {
		fmt.Printf("Error truncating log file: %v\n", err)
		log.Fatalf("Error truncating log file: %v", err)
	}

	f, err := os.OpenFile("log_server.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}

	log.SetOutput(f)
	return f
}
