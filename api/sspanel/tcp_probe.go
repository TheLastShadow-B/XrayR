package sspanel

import (
	"context"
	"errors"
	"fmt"

	"github.com/XrayR-project/XrayR/common/tcpprobe"
)

func (c *APIClient) GetTCPProbeConfig(ctx context.Context) (tcpprobe.Config, error) {
	var envelope struct {
		Ret  int             `json:"ret"`
		Data tcpprobe.Config `json:"data"`
	}
	res, err := c.client.R().SetContext(ctx).SetResult(&envelope).
		Get(fmt.Sprintf("/mod_mu/nodes/%d/tcp-probe", c.NodeID))
	if err != nil {
		return tcpprobe.Config{}, errors.New("configuration request failed")
	}
	// Older panels have no endpoint; monitoring is opt-in and must not break them.
	if res.StatusCode() == 404 {
		return tcpprobe.Config{}, nil
	}
	if res.StatusCode() != 200 || envelope.Ret != 1 {
		return tcpprobe.Config{}, fmt.Errorf("configuration rejected (HTTP %d)", res.StatusCode())
	}
	return envelope.Data, nil
}

func (c *APIClient) ReportTCPProbe(ctx context.Context, report tcpprobe.Report) error {
	var envelope struct {
		Ret int `json:"ret"`
	}
	res, err := c.client.R().SetContext(ctx).SetBody(report).SetResult(&envelope).
		Post(fmt.Sprintf("/mod_mu/nodes/%d/tcp-probe", c.NodeID))
	if err != nil {
		return errors.New("report request failed")
	}
	if res.StatusCode() != 200 || envelope.Ret != 1 {
		return fmt.Errorf("report rejected (HTTP %d)", res.StatusCode())
	}
	return nil
}
