// Package moip holds the JSON types of the moip-toolkit node API that moipctl
// reads and sends.
//
// They are copied by hand from external/moip-toolkit/server/internal/model,
// which cannot be imported from outside that module. Only the fields moipctl
// needs are kept; unknown fields in a node's response are ignored, so a newer
// toolkit does not break the controller. Update this file together with the
// submodule pin.
package moip

// State is the payload of the "state" event on GET /api/events.
type State struct {
	Sources   []NDISource     `json:"sources"`
	RX        RxStatus        `json:"rx"`
	TX        TxStatus        `json:"tx"`
	Multiview MultiviewStatus `json:"multiview"`
	Signage   SignageStatus   `json:"signage"`
	System    SystemStatus    `json:"system"`
}

// NDISource is a source found on the network.
type NDISource struct {
	Name       string  `json:"name"`
	StreamName string  `json:"stream_name"`
	HostName   string  `json:"host_name"`
	URL        *string `json:"url"`
}

type SystemStatus struct {
	CPUPercent float64    `json:"cpu_percent"`
	MemPercent float64    `json:"mem_percent"`
	MemUsedMB  float64    `json:"mem_used_mb"`
	MemTotalMB float64    `json:"mem_total_mb"`
	LoadAvg    [3]float64 `json:"load_avg"`
}

type RxStatus struct {
	Running       bool     `json:"running"`
	IsConnected   bool     `json:"is_connected"`
	CurrentSource *string  `json:"current_source"`
	RecvFmt       string   `json:"recv_fmt"`
	RecvBandwidth string   `json:"recv_bandwidth"`
	Fullscreen    bool     `json:"fullscreen"`
	VideoDriver   string   `json:"video_driver"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	FPS           float64  `json:"fps"`
	FPSReal       float64  `json:"fps_real"`
	FPSRecv       float64  `json:"fps_recv"`
	LatencyMS     *float64 `json:"latency_ms"`
	DroppedFrames int64    `json:"dropped_frames"`
	SkippedFrames int64    `json:"skipped_frames"`
	Error         *string  `json:"error"`
}

type TxStatus struct {
	Running       bool    `json:"running"`
	SenderName    *string `json:"sender_name"`
	ActualWidth   int     `json:"actual_width"`
	ActualHeight  int     `json:"actual_height"`
	ActualFPS     float64 `json:"actual_fps"`
	DroppedFrames int64   `json:"dropped_frames"`
	Connections   int     `json:"connections"`
	Error         *string `json:"error"`
}

type MultiviewSlotStatus struct {
	SourceName  string `json:"source_name"`
	IsConnected bool   `json:"is_connected"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type MultiviewStatus struct {
	Running       bool                  `json:"running"`
	Fullscreen    bool                  `json:"fullscreen"`
	AutoDiscovery bool                  `json:"auto_discovery"`
	Slots         []MultiviewSlotStatus `json:"slots"`
	Error         *string               `json:"error"`
}

type SignageStatus struct {
	Running      bool    `json:"running"`
	CurrentImage *string `json:"current_image"`
	Filename     *string `json:"filename"`
	Fullscreen   bool    `json:"fullscreen"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Output       string  `json:"output"`
	Error        *string `json:"error"`
}

// RxStartRequest is the body of POST /api/rx/start.
type RxStartRequest struct {
	SenderName    string `json:"sender_name"`
	RecvFmt       string `json:"recv_fmt"`
	RecvBandwidth string `json:"recv_bandwidth"`
	Fullscreen    bool   `json:"fullscreen"`
}

// RxSwitchRequest is the body of POST /api/rx/switch.
type RxSwitchRequest struct {
	SenderName string `json:"sender_name"`
}
