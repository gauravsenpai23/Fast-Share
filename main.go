package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

type PeerIpAndPort struct {
	ip   string `json:"ip"`
	port int    `json:"port"`
}

var udpConn *net.UDPConn

const localUdpPort = 9090

func webhookFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is supported.", http.StatusMethodNotAllowed)
		return
	}

	var peerIpAndPort PeerIpAndPort
	err := json.NewDecoder(r.Body).Decode(&peerIpAndPort)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	go startUdpHolePunching(peerIpAndPort)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte("Received ip and port of Peer"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func startUdpHolePunching(peer PeerIpAndPort) {
	peerAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", peer.ip, peer.port))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("PUNCHING: Resolved peer address to %s\n", peerAddr.String())
	localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", localUdpPort))
	if err != nil {
		fmt.Println("Error resolving local UDP address:", err)
		return
	}

	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		fmt.Println("Error listening on UDP port:", err)
		return
	}
	defer conn.Close()
	fmt.Printf("PUNCHING: Listening on local address %s\n", conn.LocalAddr().String())

	fmt.Printf("PUNCHING: Sending punch packet to %s\n", peerAddr.String())
	if _, err := conn.WriteToUDP([]byte("ping"), peerAddr); err != nil {
		fmt.Println("Error sending punch packet:", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	buffer := make([]byte, 1024)
	n, remoteAddr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		fmt.Println("Error receiving packet (or timeout):", err)
		return
	}

	fmt.Printf("SUCCESS: Hole punched! Received '%s' from %s\n", string(buffer[:n]), remoteAddr.String())
	conn.SetReadDeadline(time.Time{})

	for i := 0; i < 5; i++ {
		msg := fmt.Sprintf("hello from server %d", i)
		if _, err := conn.WriteToUDP([]byte(msg), remoteAddr); err != nil {
			fmt.Println("Error sending message:", err)
			break
		}
		fmt.Printf("--> Sent: '%s'\n", msg)
		time.Sleep(1 * time.Second)
	}

	fmt.Println("Communication finished.")
}

func main() {
	fmt.Printf("hello world")

	http.HandleFunc("/webhook", webhookFunc)

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
