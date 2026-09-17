# SSPanel 回国 TCP 检测

同步更新 SSPanel、运行 `php xcat Migration latest`，升级本版本并重启后，SSPanel 节点自动拉取 `/mod_mu/nodes/{NodeID}/tcp-probe`。在面板 **节点 → 回国检测** 配置三网公网 IPv4 目标和 TCP 端口、开启对应节点即可。不增加独立进程，不增加 API 凭证，也不需要手工同步目标到 XrayR 配置。

每轮每目标做若干次普通 TCP Connect，最大 6 目标并发，记录建连耗时和失败原因。取样次数、单次超时和轮询周期由面板的 `attempts`、`timeout_ms`、`interval_seconds` 下发；字段缺失或为 0 时回落到 3 次、3000 毫秒和按目标数推导的周期，旧面板因此无需改动。面板值会被钳制在 `attempts` 1–10、`timeout_ms` 200–10000、`interval_seconds` 60–3600，越界只降级这一项，不中断整轮测试。成功立即关闭 socket，不发送应用层数据。结果 POST 到同一接口，颜色和历史由面板计算：`threshold_ms` 是面板的着色分界线，XrayR 解析但从不消费，上报体里只有 `latency_ms` 和 `error`。目标更改在下一轮生效，不重建代理入站。

使用 `ControllerConfig.SendIP` 作为源地址；空、`0.0.0.0`、`::` 使用主机默认 IPv4 出口。测试不经过 Xray 路由规则，不能代表复杂代理分流后的业务链路。首版仅 IPv4。使用 HTTPS APIHost，并保持节点时钟同步。

未启用、目标为空或旧面板接口返回 404 时，不发起 TCP 测试。配置/上报失败只记录简短错误，不影响代理业务；控制器关闭时取消任务和在途 socket。目标可以在后台按需新增、编辑和删除，不限每家 3 个或总共 9 个。周期先按"每批 6 个目标 × 单目标最坏耗时（`attempts × timeout_ms` 加上每次取样间的 200 毫秒间隔，向上取整到秒）+ 15 秒 API 预算"算出安全下限，再取该下限与 `interval_seconds` 的较大值，最后向上取整到整分钟以保持调度对齐。所以面板只能拉长周期、不能压到一轮跑不完：默认参数下 24 个目标以内每 60 秒一轮，25–60 个目标每 120 秒一轮。配置请求最多 10 秒，测量和上报最多使用周期减 15 秒；每轮不重叠，最多 6 个工作线程。注意如果面板用 `interval_seconds` 判断数据是否过期，而目标数或超时把周期顶到了下限之上，两边的周期会分叉。本地源地址绑定失败或 FD 耗尽不作为线路中断上报。

验证：

```sh
go test ./common/tcpprobe ./api/sspanel -run 'Test(TCPProbe|Measure|RejectPrivateTargets|DisabledConfig|Tuning|Interval|Config)'
CGO_ENABLED=0 go build .
```

测试采用内存连接和本机 HTTP 服务，不连接三网实际端点。
