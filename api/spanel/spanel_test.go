package spanel

import (
	"testing"

	"github.com/XrayR-project/XrayR/api"
)

func CreateClient() api.API {
	apiConfig := &api.Config{
		APIHost:  "http://localhost:8080",
		Key:      "12345678",
		NodeID:   1,
		NodeType: "vmess",
	}
	client := New(apiConfig)
	return client
}

func TestGetV2rayNodeInfo(t *testing.T) {
	client := CreateClient()
	nodeInfo, err := client.GetNodeInfo()
	if err != nil {
		t.Error(err)
	}
	t.Log(nodeInfo)
}

func TestShadowsocks(t *testing.T) {
	apiConfig := &api.Config{
		APIHost:  "http://localhost:8080",
		Key:      "12345678",
		NodeID:   5,
		NodeType: "Shadowsocks",
	}
	client := New(apiConfig)
	nodeInfo, err := client.GetNodeInfo()
	if err != nil {
		t.Error(err)
	}
	t.Log(nodeInfo)
}
