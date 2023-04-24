package signaling

import (
	"flag"
	"html/template"
	"net/http"
	"strconv"
)

func home(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/ws")
}

func ws(w http.ResponseWriter, r *http.Request) {
	hub := newHub()
	go hub.run()

	serveWs(hub, w, r)
}

func RunHttp() {
	port := flag.Int("p", 8080, "http serve port")

	http.HandleFunc("/", home)
	http.HandleFunc("/ws", ws)

	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	if err != nil {
		panic(err)
	}
}

var homeTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>WEBRTC</title>
    <script>
        window.addEventListener("load", function (e) {
            // html
            let btnOpenCamera = document.getElementById("btn-open-camera")
            let btnCloseCamera = document.getElementById("btn-close-camera")
            let remoteVideo = document.getElementById("remote-video")

            // websocket
            let ws = new WebSocket("{{.}}")

            // webrtc config
            let rtcpc, remoteStream

            const offerOptions = {
                offerToReceiveAudio: 1,
                offerToReceiveVideo: 1
            }

            const iceServers = {
                'iceServer': [
                    {'urls': 'stun:stun.l.google.com:19302'}
                ]
            }

            // event
            btnOpenCamera.onclick = async (e) => {
                rtcpc = new RTCPeerConnection(iceServers)
                rtcpc.onsignalingstatechange = signalingStateCallback
                rtcpc.oniceconnectionstatechange = iceStateCallback
                rtcpc.onconnectionstatechange = connStateCallback
                rtcpc.onicecandidate = onIceCandidate
                rtcpc.ontrack = onAddStream
				rtcpc.addTransceiver = ("video", {
					"direction": "recvonly"
				})
                const sessionDescription = await rtcpc.createOffer(offerOptions)
                rtcpc.setLocalDescription(sessionDescription)
                ws.send(JSON.stringify({
                    topic: 'offer',
                    data: {
                        sessionDescription: sessionDescription
                    }
                }))
                console.log("offer send, data: ", sessionDescription)
            }

            btnCloseCamera.onclick = (e) => {
                ws.send(JSON.stringify({
                    topic: 'close',
                    data: ""
                }))
            }

            // websocket listen inbound message
            ws.addEventListener("open", (e) => {
                console.log("ws connection open")
            })
            ws.addEventListener("close", (e) => {
                console.log("ws connection close")
            })

            ws.addEventListener("message", (e) => {
                const eventData = JSON.parse(e.data)
                switch (eventData.topic) {
                    case "created":
                        console.log("created", eventData)
                        rtcpc.setRemoteDescription(eventData.data.answer)
                        break;
                    case "joined":
                        console.log("joined", eventData)
                        rtcpc.setRemoteDescription(eventData.data.answer)
                    default:
                        console.log("default", eventData)
                        break;
                }
            })

            // callback
            function onAddStream(e) {
                console.log("got remote stream", e)
                remoteVideo.srcObject = e.streams[0]
                remoteStream = e.streams[0]
            }

            function signalingStateCallback() {
                let state;
                if (rtcpc) {
                    state = rtcpc.signalingState
                    console.log("signaling state: ", state)
                }
            }

            function iceStateCallback() {
                let iceState
                if (rtcpc) {
                    iceState = rtcpc.iceConnectionState
                    console.log("ICE connection state: ", iceState)
                }
            }

            function connStateCallback() {
                let connState
                if (rtcpc) {
                    connState = rtcpc.connectionState
                    console.log("Connection state: ", connectionState)
                }
            }

            function onIceCandidate(e) {
                if (e.candidate) {
                    console.log('sending ice candidate', e.candidate)
                    ws.send(JSON.stringify({
                        topic: 'candidate',
                        data: {
                            label: e.candidate.sdpMLineIndex,
                            id: e.candidate.sdpMid,
                            candidate: e.candidate
                        }
                    }))
                }
            }
        })
    </script>
</head>
<body>
    <h1>Webrtc</h1>
    <button id="btn-open-camera">Open camera</button>
    <button id="btn-close-camera">Close camera</button>
    <br>
    <div>
        <video id="remote-video" autoplay playsinline></video>
    </div>
</body>
</html>
`))
