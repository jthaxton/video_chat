package main

import (

	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v2"
)

const (
	rtcpPLIInterval = time.Second * 3
)

// Sdp represent session description protocol describe media communication sessions
type Sdp struct {
	Sdp string
}

func main() {
	file, err := os.OpenFile("info.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	log.SetOutput(file)
	router := gin.Default()
	handler := Handler{AuthEndpoint: os.Getenv("AUTH_ENDPOINT")}

	m := webrtc.MediaEngine{}

	// Setup the codecs you want to use.
	// Only support VP8(video compression), this makes our proxying code simpler
	m.RegisterCodec(webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000))

	router.POST("/webrtc/sdp/m/:meetingId/c/:userID/p/:peerId/s/:isSender", handler.IsSender)

	router.Run(":8080")
}
