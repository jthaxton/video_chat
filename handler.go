package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/pion/rtcp"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v2"

)

type Handler struct {
	AuthEndpoint string
}

func (handler *Handler) Ping(ctx *gin.Context) {
	ctx.JSON(200, make(map[string]string))
}

// user is the caller of the method
// if user connects before peer: since user is first, user will create the channel and track and will pass the track to the channel
// if peer connects before user: since peer came already, he created the channel and is listning and waiting for me to create and pass track
func (handler *Handler) CreateTrack(peerConnection *webrtc.PeerConnection,
	peerConnectionMap map[string]chan *webrtc.Track,
	currentUserID string) {

	if _, err := peerConnection.AddTransceiver(webrtc.RTPCodecTypeVideo); err != nil {
		log.Fatal(err)
	}

	// Set a handler for when a new remote track starts, this just distributes all our packets
	// to connected peers
	peerConnection.OnTrack(func(remoteTrack *webrtc.Track, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		// This can be less wasteful by processing incoming RTCP events, then we would emit a NACK/PLI when a viewer requests it
		go func() {
			ticker := time.NewTicker(rtcpPLIInterval)
			for range ticker.C {
				if rtcpSendErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: remoteTrack.SSRC()}}); rtcpSendErr != nil {
					fmt.Println(rtcpSendErr)
				}
			}
		}()

		// Create a local track, all our SFU clients will be fed via this track
		// main track of the broadcaster
		localTrack, newTrackErr := peerConnection.NewTrack(remoteTrack.PayloadType(), remoteTrack.SSRC(), "video", "pion")
		if newTrackErr != nil {
			log.Fatal(newTrackErr)
		}

		// the channel that will have the local track that is used by the sender
		// the localTrack needs to be fed to the reciever
		localTrackChan := make(chan *webrtc.Track, 1)
		localTrackChan <- localTrack
		if existingChan, ok := peerConnectionMap[currentUserID]; ok {
			// feed the exsiting track from user with this track
			existingChan <- localTrack
		} else {
			peerConnectionMap[currentUserID] = localTrackChan
		}

		rtpBuf := make([]byte, 1400)
		for { // for publisher only
			i, readErr := remoteTrack.Read(rtpBuf)
			if readErr != nil {
				log.Fatal(readErr)
			}

			// ErrClosedPipe means we don't have any subscribers, this is ok if no peers have connected yet
			if _, err := localTrack.Write(rtpBuf[:i]); err != nil && err != io.ErrClosedPipe {
				log.Fatal(err)
			}
		}
	})

}

// user is the caller of the method
// if user connects before peer: create channel and keep listening till track is added
// if peer connects before user: channel would have been created by peer and track can be added by getting the channel from cache
func (handler *Handler) RecieveTrack(peerConnection *webrtc.PeerConnection,
	peerConnectionMap map[string]chan *webrtc.Track,
	peerID string) {
	if _, ok := peerConnectionMap[peerID]; !ok {
		peerConnectionMap[peerID] = make(chan *webrtc.Track, 1)
	}
	localTrack := <-peerConnectionMap[peerID]
	peerConnection.AddTrack(localTrack)
}

func (handler *Handler) IsSender(c *gin.Context) {
	m := webrtc.MediaEngine{}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	isSender, _ := strconv.ParseBool(c.Param("isSender"))
	userID := c.Param("userID")
	peerID := c.Param("peerId")

	var session Sdp
	if err := c.ShouldBindJSON(&session); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	offer := webrtc.SessionDescription{}
	Decode(session.Sdp, &offer)

	peerConnectionConfig := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	// Create a new RTCPeerConnection
	// this is the gist of webrtc, generates and process SDP
	peerConnection, err := api.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		log.Fatal(err)
	}

	// sender to channel of track
	peerConnectionMap := make(map[string]chan *webrtc.Track)
	if !isSender {
		handler.RecieveTrack(peerConnection, peerConnectionMap, peerID)
	} else {
		handler.CreateTrack(peerConnection, peerConnectionMap, userID)
	}
	// Set the SessionDescription of remote peer
	peerConnection.SetRemoteDescription(offer)

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		log.Fatal(err)
	}
	c.JSON(http.StatusOK, Sdp{Sdp: Encode(answer)})
}