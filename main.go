package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/pion/stun"
)

var tracker *cache.Cache

var apiURL = "https://n5guij-ip-117-253-83-9.tunnelmole.net/lookup?uid=%s"
var registerURL = "https://n5guij-ip-117-253-83-9.tunnelmole.net/register"

type PeerIpAndPort struct {
	Ip   string `json:"ip"`
	Port int    `json:"port"`
}

var udpConn *net.UDPConn

const senderUdpPort = 9090
const receiverUdpPort = 9091

func webhookFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is supported.", http.StatusMethodNotAllowed)
		return
	}
	fmt.Println("webhookFunc called")

	var peerIpAndPort PeerIpAndPort
	err := json.NewDecoder(r.Body).Decode(&peerIpAndPort)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	go startUdpHolePunching(peerIpAndPort, senderUdpPort)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte("Received ip and port of Peer"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func startUdpHolePunching(peer PeerIpAndPort, port int) {
	peerAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", peer.Ip, peer.Port))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("PUNCHING: Resolved peer address to %s\n", peerAddr.String())
	localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
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

func findPublicIp() (string, error) {
	// Create a new STUN client
	c, err := stun.Dial("udp4", "stun.l.google.com:19302")
	if err != nil {
		fmt.Println("Error dialing STUN server:", err)
		return "", err // Return an empty string and the error
	}
	defer c.Close()

	// Build a binding request
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	var publicIP string
	// Send the request and handle the response
	if err := c.Do(message, func(res stun.Event) {
		if res.Error != nil {
			fmt.Println("STUN request error:", res.Error)
			return
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(res.Message); err != nil {
			fmt.Println("Error getting XOR-MAPPED-ADDRESS:", err)
			return
		}
		publicIP = xorAddr.IP.String()
	}); err != nil {
		fmt.Println("Error performing STUN request:", err)
		return "", err // Return an empty string and the error
	}

	if publicIP == "" {
		// Create a new error to explain the failure
		return "", errors.New("failed to get public IP address from STUN server")
	}

	fmt.Println("My public IP is:", publicIP)
	return publicIP, nil // Return the IP and nil for the error to indicate success
}

func sendFile() {
	fmt.Println("Sending file...")
	publicIP, err := findPublicIp()
	ipAndPort := PeerIpAndPort{publicIP, senderUdpPort}
	jsonData, err := json.Marshal(ipAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	buffer := bytes.NewBuffer(jsonData)
	resp, err := http.Post(registerURL, "application/json", buffer)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	uniqueID, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Unique ID:", string(uniqueID))
}

func receiveFile() {
	fmt.Print("Input Unique Code: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Println("No input received.")
		return
	}
	uniqueCode := strings.TrimSpace(scanner.Text())
	if uniqueCode == "" {
		fmt.Println("Unique code cannot be empty.")
		return
	}
	publicIP, err := findPublicIp()
	ipAndPort := PeerIpAndPort{
		Ip:   publicIP,
		Port: 9090,
	}
	jsonData, err := json.Marshal(ipAndPort)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	bodyReader := bytes.NewBuffer(jsonData)
	resp, err := http.Post(fmt.Sprintf(apiURL, uniqueCode), "application/json", bodyReader)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		startUdpHolePunching(ipAndPort, receiverUdpPort)
		fmt.Printf("API call failed with status code: %d\n", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return
	}

	fmt.Println("Response Body:", string(body))

}

func lookupFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only Post method is supported.", http.StatusMethodNotAllowed)
	}
	uid := r.URL.Query().Get("uid")
	val, found := tracker.Get(uid)
	fmt.Println("IP address lookup function is : ", r.RemoteAddr)
	var ipAndPort PeerIpAndPort
	err := json.NewDecoder(r.Body).Decode(&ipAndPort)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !found {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	fmt.Println("Ip:", ipAndPort.Ip, " port:", ipAndPort.Port)
	fmt.Println("Found:", val)
	peerFromCache, ok := val.(PeerIpAndPort)
	if !ok {
		fmt.Println("Error: The value in the cache is not of type PeerIpAndPort")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	go callback(peerFromCache, ipAndPort)
	err = json.NewEncoder(w).Encode(val)
	if err != nil {
		fmt.Println("Error encoding JSON response:", err)
		return
	}

}

func callback(senderIpAndPort PeerIpAndPort, receiverIpAndPort PeerIpAndPort) {
	jsonData, err := json.Marshal(receiverIpAndPort)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return
	}
	buffer := bytes.NewBuffer(jsonData)
	resp, err := http.Post("http://"+senderIpAndPort.Ip+":"+strconv.Itoa(8080)+"/webhook", "application/json", buffer)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}
	if resp.StatusCode == http.StatusOK {
		fmt.Println("Received response:", string(buffer.Bytes()))
	}
}

func registerFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is supported.", http.StatusMethodNotAllowed)
	}
	fmt.Println("IP address register function is : ", r.RemoteAddr)
	var ipAndPort PeerIpAndPort
	err := json.NewDecoder(r.Body).Decode(&ipAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("ip:" + ipAndPort.Ip + " port:" + strconv.Itoa(ipAndPort.Port))
	id := uuid.NewString()
	fmt.Println("\n" + id)
	tracker.Set(id, ipAndPort, cache.DefaultExpiration)
	_, err = w.Write([]byte(id))
	if err != nil {
		fmt.Println("Error writing response:", err)
		return
	}
}

func main() {
	tracker = cache.New(10*time.Minute, 15*time.Minute)
	fmt.Println("Type these commands below:\n")
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Println("1.send\t 2.receive\t 3.quit\n")
			fmt.Print(">")
			if scanner.Scan() {
				command := strings.TrimSpace(scanner.Text())
				switch command {
				case "send":
					sendFile()
				case "receive":
					receiveFile()
					fmt.Println("Receive from server")
				case "quit":
					os.Exit(0)
				default:
					fmt.Println("Unknown command")
				}
			}
		}
	}()
	http.HandleFunc("/lookup", lookupFunc)
	http.HandleFunc("/webhook", webhookFunc)
	http.HandleFunc("/register", registerFunc)

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
