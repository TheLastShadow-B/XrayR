# SSPanel 回国 TCP 检测

同步更新 SSPanel、运行 `php xcat Migration latest`，升级本版本并重启后，SSPanel 节点自动拉取 `/mod_mu/nodes/{NodeID}/tcp-probe`。在面板 **节点 → 回国检测** 配置三网公网 IPv4 目标和 TCP 端口、开启对应节点即可。不增加独立进程，不增加 API 凭证，也不需要手工同步目标到 XrayR 配置。

每轮每目标 3 次普通 TCP Connect，单次超时 3 秒、最大 6 目标并发，记录建连耗时和失败原因。成功立即关闭 socket，不发送应用层数据。结果 POST 到同一接口，颜色和历史由面板计算。目标更改在下一轮生效，不重建代理入站。

使用 `ControllerConfig.SendIP` 作为源地址；空、`0.0.0.0`、`::` 使用主机默认 IPv4 出口。测试不经过 Xray 路由规则，不能代表复杂代理分流后的业务链路。首版仅 IPv4。使用 HTTPS APIHost，并保持节点时钟同步。

未启用、目标为空或旧面板接口返回 404 时，不发起 TCP 测试。配置/上报失败只记录简短错误，不影响代理业务；控制器关闭时取消任务和在途 socket。目标可以在后台按需新增、编辑和删除，不限每家 3 个或总共 9 个。最多 24 个目标时每 60 秒一轮，更多目标自动延长至整分钟周期：每批 6 个目标预留 10 秒，并预留 API 时间。例如 25–60 个目标每 120 秒一轮。配置请求最多 10 秒，测量和上报最多使用周期减 15 秒；每轮不重叠，最多 6 个工作线程。本地源地址绑定失败或 FD 耗尽不作为线路中断上报。

验证：

```sh
go test ./common/tcpprobe ./api/sspanel -run 'Test(TCPProbe|Measure|RejectPrivateTargets|DisabledConfig)'
CGO_ENABLED=0 go build .
```

测试采用内存连接和本机 HTTP 服务，不连接三网实际端点。
