package main

import (
	"encoding/xml"
	"fmt"
)

// SystemInfo represents VEEAM SOSAPI system.xml structure
type SystemInfo struct {
	XMLName               xml.Name              `xml:"SystemInfo"`
	ProtocolVersion       string                `xml:"ProtocolVersion"`
	ModelName             string                `xml:"ModelName"`
	ProtocolCapabilities  ProtocolCapabilities  `xml:"ProtocolCapabilities"`
	SystemRecommendations SystemRecommendations `xml:"SystemRecommendations"`
}

// ProtocolCapabilities defines supported SOSAPI features
type ProtocolCapabilities struct {
	CapacityInfo   bool `xml:"CapacityInfo"`
	UploadSessions bool `xml:"UploadSessions"`
	IAMSTS         bool `xml:"IAMSTS"`
}

// SystemRecommendations provides storage system recommendations
type SystemRecommendations struct {
	S3ConcurrentTaskLimit     int  `xml:"S3ConcurrentTaskLimit,omitempty"`
	S3MultiObjectDeleteLimit  int  `xml:"S3MultiObjectDeleteLimit,omitempty"`
	StorageCurrentTaskLimit   int  `xml:"StorageCurrentTaskLimit,omitempty"`
	KBBlockSize               int  `xml:"KbBlockSize"`
	IsMultiBucketModeRequired bool `xml:"IsMultiBucketModeRequired"`
}

func main() {
	sysInfo := SystemInfo{
		ProtocolVersion: "1.0",
		ModelName:       "MinIO",
		ProtocolCapabilities: ProtocolCapabilities{
			CapacityInfo:   true,
			UploadSessions: false,
			IAMSTS:         false,
		},
		SystemRecommendations: SystemRecommendations{
			KBBlockSize:               4096,
			IsMultiBucketModeRequired: false,
		},
	}

	output, err := xml.MarshalIndent(sysInfo, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println("=== SOSAPI system.xml ===")
	fmt.Println(xml.Header + string(output))
	fmt.Println("\n=== Verification ===")
	fmt.Printf("IsMultiBucketModeRequired: %v\n", sysInfo.SystemRecommendations.IsMultiBucketModeRequired)
}
