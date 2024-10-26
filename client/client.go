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
	"sync"

	gRPC "github.com/tcapook01/chitty-chat/proto"
	"google.golang.org/grpc"
)

func main() {
	// Flags for user-specific arguments
	name := flag.String("name", "default", "Participant name")
	serverAddr := flag.String("server", "localhost:5400", "Server address")
	flag.Parse()

	fmt.Println("--- CLIENT APP ---")

	// Connect to the server
	fmt.Println("--- Connecting to Server ---")
	conn, err := grpc.Dial(*serverAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()
	client := gRPC.NewChittyChatClient(conn)

	// Create a bidirectional message stream
	stream, err := client.MessageStream(context.Background())
	if err != nil {
		log.Fatalf("Failed to create message stream: %v", err)
	}

	// Create a cancellable contex to manage goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	//Goroutine to receive messages from the server
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				fmt.Println("Server closed the stream")
				cancel() // Cancel the context to signal sending goroutine to exit
				return
			}
			if err != nil {
				log.Printf("Failed to recieve the message: %v", err)
				cancel() // Cancel the context to signal sending goroutine to exit
				return
			}

			switch payload := msg.Payload.(type) {
			case *gRPC.ServerMessage_Broadcast:
				fmt.Printf("[%s] %s\n", payload.Broadcast.ParticipantName, payload.Broadcast.Message)
			case *gRPC.ServerMessage_Ack:
				fmt.Printf("[Ack] Success: %v, Info: %s\n", payload.Ack.Success, payload.Ack.Info)
			default:
				fmt.Println("Unknown message type received.")
			}
		}
	}()

	// Goroutine to send messages to the server
	go func() {
		defer wg.Done()

		// Send JoinRequest
		joinMsg := &gRPC.ClientMessage{
			Payload: &gRPC.ClientMessage_Join{
				Join: &gRPC.JoinRequest{
					ParticipantName: *name,
				},
			},
		}
		if err := stream.Send(joinMsg); err != nil {
			log.Printf("Error sending join message: %v", err)
			cancel()
			return
		}

		// Read from stdin and send PublishRequest or LeaveRequest
		scanner := bufio.NewScanner(os.Stdin)
		for {
			select {
			case <-ctx.Done():
				// Context canceled, exit the sending goroutine
				return
			default:
			}

			if !scanner.Scan() {
				// Input closed (e.g., EOF), initiate graceful exit
				leaveMsg := &gRPC.ClientMessage{
					Payload: &gRPC.ClientMessage_Leave{
						Leave: &gRPC.LeaveRequest{
							ParticipantName: *name,
						},
					},
				}
				if err := stream.Send(leaveMsg); err != nil {
					log.Printf("Error sending leave message: %v", err)
				}
				fmt.Println("Exiting chat. Goodbye!")
				stream.CloseSend()
				cancel() // Cancel context to signal receiving goroutine to exit
				return
			}

			text := scanner.Text()
			if strings.ToUpper(text) == "EXIT" {
				// Send LeaveRequest
				leaveMsg := &gRPC.ClientMessage{
					Payload: &gRPC.ClientMessage_Leave{
						Leave: &gRPC.LeaveRequest{
							ParticipantName: *name,
						},
					},
				}
				if err := stream.Send(leaveMsg); err != nil {
					log.Printf("Error sending leave message: %v", err)
				}
				fmt.Println("Exiting chat. Goodbye!")
				stream.CloseSend()
				cancel() // Cancel context to signal receiving goroutine to exit
				return
			}

			// Send PublishRequest
			publishMsg := &gRPC.ClientMessage{
				Payload: &gRPC.ClientMessage_Publish{
					Publish: &gRPC.PublishRequest{
						ParticipantName: *name,
						Message:         text,
						LamportTime:     0, // The server will handle Lamport time
					},
				},
			}
			if err := stream.Send(publishMsg); err != nil {
				log.Printf("Error sending publish message: %v", err)
				cancel()
				return
			}
		}
	}()

	wg.Wait()
}
