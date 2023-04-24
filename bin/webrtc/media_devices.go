package wrtc

import (
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/prop"
)

func mediaStreamInit() {
	stream, _ := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(constraint *mediadevices.MediaTrackConstraints) {
			constraint.Width = prop.Int(600)
			constraint.Height = prop.Int(400)
		},
	})

	track := stream.GetVideoTracks()[0]
	videoTrack := track.(*mediadevices.VideoTrack)
	defer videoTrack.Close()
}
