window.addEventListener("load", function (e) {
    // global websocket instance
    let ws, sendChannel
    // html
    let btnOpenCamera = document.querySelector(".btn-open-camera")
    let btnCloseCamera = document.getElementById("btn-close-camera")
    let remoteVideo = document.querySelecor(".remote-video")
    let cameraCard = document.querySelector('camera-card')
    let baseVideoWrapper = document.querySelector('.base-video-wrapper')
    
    //baseVideoWrapper.style.display = none;
    console.log(baseVideoWrapper)
  
    // webrtc config
    let rtcpc

    const offerOptions = {
        offerToReceiveVideo: 1
    }

    const iceServers = {
        'iceServer': [
            {'urls': 'stun:stun.l.google.com:19302'},
            {'urls': 'stun:stun.services.mozilla.com'}
        ]
    }

       //  // websocket
       ws = new WebSocket("{{.}}")

        ws.onopen = (e) => {
            console.log("ws open")
        }

        ws.onclose = (e) => {
            console.log("ws close")
        }

        ws.onmessage = (e) => {
            let msg = JSON.parse(e.data)
            switch (msg.topic) {
                case 'candidate':
                    console.log('ice candidate-raspberry', JSON.parse(msg.data))
                    const candidate = new RTCIceCandidate(JSON.parse(msg.data))
                    rtcpc.addIceCandidate(candidate)
                        .catch(err => console.log(err))
                    break
                case 'answer':
                    console.log('answer coming')
                    rtcpc.setRemoteDescription(JSON.parse(msg.data))
                    break;
                default:
                    break;
            }
        }

    // event
    btnOpenCamera.onclick = async (e) => {
        rtcpc = new RTCPeerConnection(iceServers)
        rtcpc.onsignalingstatechange = signalingStateCallback
        rtcpc.oniceconnectionstatechange = iceStateCallback
        rtcpc.onconnectionstatechange = connStateCallback
        rtcpc.onicecandidate = onIceCandidate
        rtcpc.ontrack = onAddStream
        rtcpc.addTransceiver('video', {
            'direction': 'recvonly'
        })
        sendChannel = rtcpc.createDataChannel('foo')
        sendChannel.onclose = () => console.log('sendChannel has closed')
        sendChannel.onopen = () => console.log('sendChannel has opened')
        sendChannel.onmessage = e => console.log(`Message from DataChannel '${sendChannel.label}' payload '${e.data}'`)
        const sessionDescription = await rtcpc.createOffer()
        rtcpc.setLocalDescription(sessionDescription)
        console.log("offer sending: ", sessionDescription)
        ws.send(JSON.stringify({
            topic: 'offer',
            data: JSON.stringify(sessionDescription)
        }))
    }

    // callback
    function onAddStream(e) {
        console.log("got remote stream")
        baseVideoWrapper.style.display = flex;
        cameraCard.style.display = none
        el = document.createElement(e.track.kind)
        el.srcObject = e.streams[0]
        el.controls = true
        el.autoplay = true
        remoteVideo.appendChild(el)
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
        if (e.candidate != null & ws != null) {
            offerSD = rtcpc.localDescription
            console.log('ice candidate-client', e.candidate, "\noffer: ",offerSD)
            ws.send(JSON.stringify({
                topic: 'candidate',
                data: JSON.stringify(offerSD)
            }))
        }
    }

    // // DATA CHANNEL


})
