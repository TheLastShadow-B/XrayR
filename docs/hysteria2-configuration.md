---
title: "Hysteria 2 Configuration Guide"
type: guide
status: active
date: 2026-04-24
---

# Hysteria 2 Configuration Guide

Hysteria 2 (Hy2) is a QUIC-based proxy protocol with BBR-style congestion
control and optional Salamander obfuscation. XrayR provisions Hy2 nodes
through the SSPanel `custom_config` path; this guide walks through the
panel-side and XrayR-side wiring and the most common startup errors.

## Prerequisites

- **Panel**: SSPanel-UIM with Hy2 support registered as `sort = 15`
  (per `docs/plans/2026-04-24-001-feat-hysteria2-support-plan.md`, R2).
- **XrayR**: version ≥ 1.0.0 (this repo; Hy2 parsing lands in
  `api/sspanel/sspanel.go` `ParseSSPanelNodeInfo`; inbound in
  `service/controller/inboundbuilder.go` `buildHysteria2StreamSettings`).
- **TLS certificate**: Hy2 always runs over TLS. `CertMode: none` is not
  valid for Hy2 and will fail at inbound build time.
- **UDP reachability**: Hy2 runs over QUIC/UDP. Open the node's UDP port
  in the host firewall and cloud security group. The TCP port of the same
  number is not used by Hy2.
- **HTTPS ApiHost**: plain `http://` panel URLs trigger a startup warning
  because the panel API key (`muKey`) is sent in the query string.

## Panel side (SSPanel-UIM)

1. Create a node whose `sort` is `15` (Hysteria 2).
2. Set the node's `custom_config` JSON. Hy2-specific knobs live inside a
   nested `Hy2Opts` object (the field is JSON-tagged `Hy2Opts` — case
   matters).

### Minimal `custom_config`

```json
{
  "offset_port_node": "443",
  "host": "node1.example.com"
}
```

This yields a working Hy2 node with no obfuscation, no masquerade
override, and no panel-side bandwidth caps. XrayR substitutes a safe
default masquerade (`type: string`, `statusCode: 404`, empty body) that
does not emit outbound traffic.

### Full `custom_config` with all Hy2 features

```json
{
  "offset_port_node": "443",
  "offset_port_user": "443",
  "host": "node1.example.com",
  "Hy2Opts": {
    "up_mbps": 500,
    "down_mbps": 1000,
    "obfs": "salamander",
    "obfs_password": "strong_random_secret",
    "masquerade": {
      "type": "url",
      "url": "https://www.bing.com/",
      "rewrite_host": true,
      "insecure": false
    }
  }
}
```

### `Hy2Opts` field reference

| Field | Type | Required | Notes |
|---|---|---|---|
| `up_mbps` | uint32 | optional | Server→client bandwidth cap for BBR (Mbps). `0` leaves it unset; XrayR emits `brutalUp` only when > 0. |
| `down_mbps` | uint32 | optional | Client→server bandwidth cap (Mbps). Same semantics as `up_mbps`. |
| `obfs` | string | optional | `""` (no obfs) or `"salamander"`. Anything else is rejected. |
| `obfs_password` | string | **required when `obfs = "salamander"`** | Shared secret between server and all clients for this node. Redacted from XrayR error logs. |
| `masquerade` | object | optional | HTTP/3 decoy response for probing traffic. See next table. |

#### `masquerade.*` sub-fields

| Field | Type | Required | Notes |
|---|---|---|---|
| `type` | string | required when `masquerade` set | `"url"`, `"file"`, or `"string"`. Anything else fails the parser with `masquerade.type must be one of url\|file\|string`. |
| `url` | string | required for `type=url` | Upstream origin that Hy2 reverse-proxies to for probing requests. |
| `rewrite_host` | bool | optional (url mode) | Rewrite the `Host` header to the upstream hostname. |
| `insecure` | bool | optional (url mode) | Skip TLS verify on the upstream fetch. Use only for self-signed backends. |
| `dir` | string | required for `type=file` | Local directory served as static content. Xray-core calls this `dir`, not `file`. |
| `content` | string | required for `type=string` | Inline response body. |
| `status_code` | int32 | optional (string mode) | HTTP status returned for the static body. Default `404`. |

## XrayR side (`/etc/XrayR/config.yml`)

Add or modify one `Nodes` entry. Only the Hy2-specific parts differ from
other SSPanel entries — `ApiConfig.NodeType: Hysteria2` is the trigger,
and `CertConfig` must resolve to a real certificate.

```yaml
Nodes:
  - PanelType: "SSpanel"
    ApiConfig:
      ApiHost: "https://panel.example.com"
      ApiKey:  "YOUR_MU_KEY"
      NodeID:  15
      NodeType: Hysteria2          # <-- selects the Hy2 parse + inbound path
      Timeout: 30
      DisableCustomConfig: false   # must be false; Hy2 lives in custom_config
    ControllerConfig:
      ListenIP: 0.0.0.0
      SendIP:   0.0.0.0
      UpdatePeriodic: 60
      CertConfig:
        CertMode:   dns            # none is NOT supported for Hy2
        CertDomain: "node1.example.com"
        Provider:   alidns
        Email:      admin@example.com
        DNSEnv:
          ALICLOUD_ACCESS_KEY: "aaa"
          ALICLOUD_SECRET_KEY: "bbb"
```

### Choosing a `CertMode`

Hy2 always runs over TLS, so the inbound builder demands a usable
certificate at startup. The four modes behave as follows:

| `CertMode` | When to use |
|---|---|
| `file` | You already have a cert + key on disk. Fill `CertFile` and `KeyFile`. |
| `http` | ACME HTTP-01. Requires TCP :80 reachable. Hy2 itself uses UDP, so HTTP-01 works, but you still need :80 open. |
| `dns` | ACME DNS-01. Recommended for Hy2 because you can issue wildcard certs and do not need :80. Set `Provider`, `Email`, and `DNSEnv` per [lego provider docs](https://go-acme.github.io/lego/dns/). |
| `none` | **Not allowed for Hy2.** Startup will fail. |

### Legacy SSPanel caveat

XrayR's SSPanel adapter splits requests into two paths:

- **Legacy mod_mu path** (panel version < 2021.11 or `DisableCustomConfig: true`) — only supports `V2ray` / `Trojan` / `Shadowsocks` / `Shadowsocks-Plugin`. Asking for `NodeType: Hysteria2` here will fail with `unsupported Node type: Hysteria2`.
- **`custom_config` path** (panel ≥ 2021.11, default) — parses `Hy2Opts` and is the only branch that supports Hy2.

If your panel is older than 2021.11, upgrade it before configuring Hy2.

## Verifying the deployment

```bash
sudo systemctl restart XrayR
sudo journalctl -u XrayR -n 100 --no-pager
```

Successful startup emits lines similar to:

```
level=info msg="Start the panel.."
level=info msg="Added new tag: SSpanel_..._Hysteria2_443"
```

Then from a client (Nekoray / NekoBox / v2rayN with Hy2 support):

1. Import the Hy2 profile from the panel's subscription or build a
   `hy2://` URI manually.
2. Confirm the client negotiates UDP to the node's port.
3. Confirm traffic routes through.
4. If `up_mbps` / `down_mbps` are configured, confirm speed caps apply
   under load.

## Common errors

| Log line | Cause | Fix |
|---|---|---|
| `unsupported Node type: Hysteria2` | Panel returning legacy mod_mu format. | Upgrade SSPanel to ≥ 2021.11 or stop using `DisableCustomConfig: true`. |
| `Hysteria2: obfs="salamander" requires non-empty obfs_password in custom_config.Hy2Opts` | Salamander enabled without a password. | Set `Hy2Opts.obfs_password` in the panel. |
| `Hysteria2: masquerade.type must be one of url\|file\|string, got "…"` | Typo in `masquerade.type`. | Use one of the three literal values. |
| `dial tcp …: connect: connection refused` during `Panel Start` | Unrelated to Hy2 — panel itself is unreachable. | Check `ApiHost`, panel service, firewall. |
| `CertMode: none` inbound build fails | Hy2 requires TLS. | Switch to `file` / `http` / `dns`. |

## Security notes

- `obfs_password` is redacted (`"obfs_password":"[REDACTED]"`) from XrayR
  error logs and panel-response dumps, so logs are safe to paste into
  issue trackers.
- `allowInsecure` in node-level TLS config is parsed for backward
  compatibility but never acted on by XrayR; Xray-core v26.x is
  scheduled to hard-reject it after 2026-06-01. Replace with a proper
  cert.
- Plain `http://` in `ApiHost` sends `ApiKey` / `muKey` over the wire in
  cleartext. XrayR logs a warning at startup; treat it as a must-fix in
  production.

## References

- Plan: `docs/plans/2026-04-24-001-feat-hysteria2-support-plan.md`
- Requirements: `docs/brainstorms/2026-04-24-hysteria2-revised-requirements.md`
- Parser: `api/sspanel/sspanel.go` (`ParseSSPanelNodeInfo`, case `"Hysteria2"`)
- Inbound builder: `service/controller/inboundbuilder.go` (`buildHysteria2StreamSettings`)
- API model: `api/apimodel.go` (`Hy2MasqueradeCfg`, `UpMbps`, `DownMbps`, `Obfs`, `ObfsPassword`)
- Panel response model: `api/sspanel/model.go` (`Hy2OptsStruct`, `Hy2MasqueradeOpts`)
