package signaling

type Hub struct {
	// registered clients
	clients map[string]*Client

	// inbound message from the clients
	broadcast chan []byte

	// register request from the client
	register chan *Client

	// unregistered request from the client
	unregistered chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcast:    make(chan []byte),
		register:     make(chan *Client),
		unregistered: make(chan *Client),
		clients:      make(map[string]*Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client.id] = client
		case client := <-h.unregistered:
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.send)
			}
		case message := <-h.broadcast:
			for _, client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client.id)
				}
			}
		}
	}
}
