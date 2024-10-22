package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	gRPC "github.com/tcapook01/chitty-chat/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Flags for user-specific arguments
var clientName = flag.String("name", "default", "Sender's name")
var serverPort = flag.String("server", "5400", "TCP server port")

var serverConn *grpc.ClientConn      // Server connection
var chatClient gRPC.ChittyChatClient // Chat server client
var lamportTime int64 = 0            // Lamport timestamp

func main() {
	// Parse command-line flags
	flag.Parse()

	fmt.Println("--- CLIENT APP ---")

	// Set up logging to a file
	logFile := setLog()
	defer logFile.Close()

	// Connect to the server
	fmt.Println("--- Connecting to Server ---")
	connectToServer()
	defer serverConn.Close()

	// Create a bidirectional message stream
	chatStream, err := chatClient.MessageStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating message stream: %v", err)
	}

	// Send join message to the chat
	sendJoinMessage(chatStream)

	// Start listening for incoming messages in a goroutine
	go listenForMessages(chatStream)

	// Start parsing user input
	parseInput(chatStream)
}

// Function to connect to the server
func connectToServer() {
	opts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // Insecure for local testing
	}

	var err error
	serverConn, err = grpc.Dial(fmt.Sprintf("localhost:%s", *serverPort), opts...)
	if err != nil {
		log.Fatalf("Error connecting to server: %v", err)
	}

	chatClient = gRPC.NewChittyChatClient(serverConn)
	fmt.Println("Connected to server.")
}

// Function to parse user input
func parseInput(stream gRPC.ChittyChat_MessageStreamClient) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Welcome to Chitty Chat!")
	fmt.Println("--------------------------")

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Error reading input: %v", err)
		}
		input = strings.TrimSpace(input)

		if len(input) > 128 {
			fmt.Println("Message is too long. It must be under 128 characters.")
			continue
		}

		if input == "exit" {
			// Send leave message to the chat
			sendLeaveMessage(stream)
			os.Exit(0)
		} else {
			// Send message to the chat
			sendChatMessage(input, stream)
		}
	}
}

// Function to set up the logger to write to a log file
func setLog() *os.File {
	logFileName := "log_" + *clientName + ".txt"
	f, err := os.OpenFile(logFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}
	log.SetOutput(f)
	return f
}

// Function to send a join message to the server
func sendJoinMessage(stream gRPC.ChittyChat_MessageStreamClient) {
	joinMsg := &gRPC.BroadcastMessage{
		Message:         fmt.Sprintf("Participant %s joined the chat", *clientName),
		ParticipantName: *clientName,
		LamportTime:     incrementLamport(),
	}
	if err := stream.Send(joinMsg); err != nil {
		log.Fatalf("Error sending join message: %v", err)
	}
}

// Function to send a leave message to the server
func sendLeaveMessage(stream gRPC.ChittyChat_MessageStreamClient) {
	leaveMsg := &gRPC.BroadcastMessage{
		Message:         fmt.Sprintf("Participant %s left the chat", *clientName),
		ParticipantName: *clientName,
		LamportTime:     incrementLamport(),
	}
	if err := stream.Send(leaveMsg); err != nil {
		log.Printf("Error sending leave message: %v", err)
	}
}

// Function to send a chat message to the server
func sendChatMessage(content string, stream gRPC.ChittyChat_MessageStreamClient) {
	chatMsg := &gRPC.BroadcastMessage{
		Message:         content,
		ParticipantName: *clientName,
		LamportTime:     incrementLamport(),
	}
	if err := stream.Send(chatMsg); err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

// Function to listen for incoming messages from the server
func listenForMessages(stream gRPC.ChittyChat_MessageStreamClient) {
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Println("Stream closed by server.")
			break
		}
		if err != nil {
			log.Printf("Error receiving message: %v", err)
			break
		}

		// Update Lamport timestamp
		updateLamport(msg.LamportTime)

		// Display and log the received message
		if msg.Message != "" {
			fmt.Printf("[%d] %s: \"%s\"\n", msg.LamportTime, msg.ParticipantName, msg.Message)
			log.Printf("[%d] %s: \"%s\"\n", msg.LamportTime, msg.ParticipantName, msg.Message)
		}
	}
}

// Function to increment Lamport timestamp when sending a message
func incrementLamport() int64 {
	lamportTime++
	return lamportTime
}

// Function to update Lamport timestamp when receiving a message
func updateLamport(received int64) {
	if received > lamportTime {
		lamportTime = received + 1
	} else {
		lamportTime++
	}
}
