# 手动运行说明
大部分情况通过<https://console.openp2p.cn> 操作即可。有些情况需要手动运行  
> :warning: 本文所有命令, Windows环境使用"openp2p.exe", Linux环境使用"./openp2p"


## 安装和监听
```
./openp2p install -node OFFICEPC1 -token TOKEN  
或
./openp2p -d -node OFFICEPC1 -token TOKEN  
# 注意Windows系统把“./openp2p” 换成“openp2p.exe”
```
>* install: 安装模式【推荐】，会安装成系统服务，这样它就能随系统自动启动
>* -d: daemon模式。发现worker进程意外退出就会自动启动新的worker进程
>* -node: 独一无二的节点名字，唯一标识
>* -token: 在<console.openp2p.cn>“我的”里面找到
>* -sharebandwidth: 作为共享节点时提供带宽，默认10mbps. 如果是光纤大带宽，设置越大效果越好. 0表示不共享，该节点只在私有的P2P网络使用。不加入共享的P2P网络，这样也意味着无法使用别人的共享节点
>* -loglevel: 需要查看更多调试日志，设置0；默认是1

### 在docker容器里运行openp2p
我们暂时还没提供官方docker镜像，你可以在随便一个容器里运行
```
nohup ./openp2p -d -node OFFICEPC1 -token TOKEN  &
#这里由于一般的镜像都精简过，install系统服务会失败，所以使用直接daemon模式后台运行
```
## 连接
```
./openp2p -d -node HOMEPC123 -token TOKEN -appname OfficeWindowsRemote -peernode OFFICEPC1 -dstip 127.0.0.1 -dstport 3389 -srcport 23389
使用配置文件，建立多个P2PApp
./openp2p -d   
```
>* -appname: 这个P2P应用名字
>* -peernode: 目标节点名字
>* -dstip: 目标服务地址，默认本机127.0.0.1
>* -dstport: 目标服务端口，常见的如windows远程桌面3389，Linux ssh 22
>* -protocol: 目标服务协议 tcp、udp

## 配置文件
一般保存在当前目录，安装模式下会保存到 `C:\Program Files\OpenP2P\config.json` 或 `/usr/local/openp2p/config.json`
希望修改参数，或者配置多个P2PApp可手动修改配置文件

配置实例
```
{
  "network": {
    "Node": "YOUR-NODE-NAME",
    "Token": "TOKEN",
    "ShareBandwidth": 0,
    "ServerHost": "api.openp2p.cn",
    "ServerPort": 27183,
    "UDPPort1": 27182,
    "UDPPort2": 27183
  },
  "apps": [
    {
      "AppName": "OfficeWindowsPC",
      "Protocol": "tcp",
      "SrcPort": 23389,
      "PeerNode": "OFFICEPC1",
      "DstPort": 3389,
      "DstHost": "localhost",
    },
    {
      "AppName": "OfficeServerSSH",
      "Protocol": "tcp",
      "SrcPort": 22,
      "PeerNode": "OFFICEPC1",
      "DstPort": 22,
      "DstHost": "192.168.1.5",
    }
  ]
}
```

## 获取无额外数据的直连 TCP 隧道

某些应用（比如自定义协议或设备）要求直连 TCP 隧道里只能出现业务侧原始字节。可以通过 `disableTCPKeepalive` 功能关闭 OpenP2P 的保活/心跳帧，并在双方之间协商启用“原始直通”传输模式。

1. **确保双方都升级到支持该功能的版本。** `disableTCPKeepalive` 需要新版本客户端才能理解推送协商消息，旧版本会忽略选项并继续使用带帧的兼容模式。
2. **在两个节点上都启用 `--kk` 选项。** 例如：
   ```bash
   ./openp2p -d --kk -node NODE_A -token TOKEN_A
   ./openp2p -d --kk -node NODE_B -token TOKEN_B
   ```
   `--kk` 会在内存配置里设置 `disableTCPKeepalive=true`，直连 TCP 隧道建立后不会主动发送任何心跳包。
3. **或在配置文件里长期启用。** 在 `config.json` 顶层加入：
   ```json
   {
     "disableTCPKeepalive": true,
     "network": { ... },
     "apps": [ ... ]
   }
   ```
   这样即使通过 `./openp2p -d` 从配置启动，也会自动带上该偏好。
4. **等待双方完成原始传输协商。** 当两个节点都开启了该选项并成功打通直连 TCP 隧道时，日志会出现类似 `tunnel entering raw direct mode` 的调试信息，之后 `GET /` 等业务数据会直接沿用系统 TCP 流复制，不再穿插任何控制帧。
5. **如果需要让隧道表现为 HTTP 流量，可注入一段请求头。** 在负责主动发起 TCP 连接的一端同时带上 `--kk` 和 `--Host=example.com`（把 `example.com` 改成希望展示的 Host）：
   ```bash
   ./openp2p -d --kk --Host=example.com -node NODE_A -token TOKEN_A
   ```
   原始隧道建立成功后，OpenP2P 会在转发业务数据前额外发送一行 `GET / HTTP/1.1\r\nHost: example.com\r\n\r\n`，使得直连看起来像普通的 HTTP 请求。

> ⚠️ 如果任一节点未启用 `disableTCPKeepalive`、原始会话握手 5 秒内未完成，或隧道被迫转为中继，OpenP2P 会自动退回默认的带帧通道继续通信，以保证连接稳定性。

## 升级客户端
```
# update local client
./openp2p update
# update remote client
curl --insecure 'https://api.openp2p.cn:27183/api/v1/device/YOUR-NODE-NAME/update?user=&password='
```

Windows系统需要设置防火墙放行本程序，程序会自动设置，如果设置失败会影响连接功能。
Linux系统（Ubuntu和CentOS7）的防火墙默认配置均不会有影响，如果不行可尝试关闭防火墙
```
systemctl stop firewalld.service
systemctl start firewalld.service
firewall-cmd --state
```
## 停止
TODO: windows linux macos
## 卸载
```
./openp2p uninstall
# 已安装时
# windows
C:\Program Files\OpenP2P\openp2p.exe uninstall
# linux,macos
sudo /usr/local/openp2p/openp2p uninstall
```

## Docker运行
```
# 把YOUR-TOKEN和YOUR-NODE-NAME替换成自己的
docker run -d --restart=always --net host --name openp2p-client -e OPENP2P_TOKEN=YOUR-TOKEN -e OPENP2P_NODE=YOUR-NODE-NAME  openp2pcn/openp2p-client:latest 
OR
docker run -d --restart=always --net host --name openp2p-client  openp2pcn/openp2p-client:latest -token YOUR-TOKEN -node YOUR-NODE-NAME
```
