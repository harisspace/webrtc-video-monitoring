package signaling

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/harisspace/go-webrtc/bin/utils"
	"github.com/pion/webrtc/v3"
)

// global
var upgrader = websocket.Upgrader{}
var SessionDescriptionOffer = make(chan []byte)
var SessionDescriptionAnswer = make(chan webrtc.SessionDescription)
var IceCandidate = make(chan webrtc.ICECandidate)

type Client struct {
	hub *Hub

	// websocket connection
	conn *websocket.Conn

	// buffered channel of outbound message
	send chan []byte

	// client id
	id string
}

func (c *Client) writeMsg() {
	defer func() {
		c.conn.Close()
	}()

	for msg := range c.send {
		w, err := c.conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		w.Write(msg)
		if err := w.Close(); err != nil {
			return
		}
		log.Println("send: ", msg)
	}
}

func (c *Client) readMsg() {
	defer func() {
		c.conn.Close()
	}()

	var msgRes utils.BaseMessage
	var msgSend utils.BaseMessage

	for {
		_, msg, err := c.conn.ReadMessage()

		switch json.Unmarshal(msg, &msgRes); msgRes.Topic {
		case "offer":
			numClient := len(c.hub.clients)
			if numClient == 1 {
				log.Printf("created, data : %v", msgRes.Data)
				msgSend.Topic = "created"
				offerSD, _ := json.Marshal(msgRes.Data["sessionDescription"])
				log.Printf("offerSD data : %v \n", offerSD)
				go func() {
					SessionDescriptionOffer <- offerSD
				}()
				go NewWRTC()
				answer := <-SessionDescriptionAnswer
				msgSend.Data = make(map[string]interface{})
				msgSend.Data["answer"] = answer
				data, err := json.Marshal(msgSend)
				if err != nil {
					log.Fatal(err)
				}
				c.send <- data
				log.Println("newrtc done")
			} else if numClient >= 2 {
				log.Printf("joined, data : %v", msgRes.Data)
				msgSend.Topic = "joined"
				offerSD, _ := json.Marshal(msgSend)
				c.send <- offerSD
			}
		case "close":
			log.Println("close")
			msgSend.Topic = "close"
			data, _ := json.Marshal(msgSend)
			c.hub.broadcast <- data
		case "candidate":
			log.Println("candidate", msgRes.Data)
			candidate := webrtc.ICECandidateInit{}
			v, _ := json.Marshal(msgRes.Data["candidate"])
			json.Unmarshal(v, &candidate)
			addIceCandidate(candidate)
		default:
			log.Println("default", msgRes.Topic)
		}

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade failed", err)
		return
	}

	id := uuid.NewString()
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), id: id}
	client.hub.register <- client

	go client.writeMsg()
	go client.readMsg()
}
