package main

import (
	"log"
	"net/http"
	"os"
)

type appConfig struct {
	Addr            string
	RTMPBase        string
	FLVBase         string
	HLSBase         string
	WhipBase        string
	WhepBase        string
	SRSAPIBase      string
	FFmpegBin       string
	JoinTokenSecret string
}

func configFromEnv() appConfig {
	return appConfig{
		Addr:            envOrDefault("CONTROL_PLANE_ADDR", ":8081"),
		RTMPBase:        envOrDefault("SRS_RTMP_BASE", "rtmp://127.0.0.1:1935/live"),
		FLVBase:         envOrDefault("SRS_FLV_BASE", "http://127.0.0.1:8080/live"),
		HLSBase:         envOrDefault("SRS_HLS_BASE", "http://127.0.0.1:8080/live"),
		WhipBase:        envOrDefault("SRS_WHIP_BASE", defaultWhipBase),
		WhepBase:        envOrDefault("SRS_WHEP_BASE", defaultWhepBase),
		SRSAPIBase:      envAllowEmpty("SRS_API_BASE", "http://127.0.0.1:1985"),
		FFmpegBin:       envOrDefault("FFMPEG_BIN", "ffmpeg"),
		JoinTokenSecret: os.Getenv("JOIN_TOKEN_SECRET"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// envAllowEmpty lets an explicitly empty value disable an optional feature.
func envAllowEmpty(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	config := configFromEnv()

	var probe IngestProbe
	if config.SRSAPIBase != "" {
		probe = NewSRSProbe(config.SRSAPIBase)
	}

	service := NewBroadcastService(
		ExecRunner{Bin: config.FFmpegBin},
		probe,
		ServiceConfig{
			RTMPBase: config.RTMPBase,
			FLVBase:  config.FLVBase,
			HLSBase:  config.HLSBase,
		},
	)

	rooms, err := NewRoomSession(RoomConfig{
		JoinSecret: []byte(config.JoinTokenSecret),
		WhipBase:   config.WhipBase,
		WhepBase:   config.WhepBase,
	})
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{Addr: config.Addr, Handler: newMux(service, rooms)}

	log.Printf("control-plane listening on %s", config.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
