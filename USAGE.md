

# Parameters details
In most cases, you can operate it through <https://console.openp2p.cn>. In some cases it is necessary to run manually
> :warning: all commands in this doc, Windows env uses "openp2p.exe", Linux env uses "./openp2p" 


## Install and Listen
```
./openp2p install -node OFFICEPC1 -token TOKEN  
Or
./openp2p -d -node OFFICEPC1 -token TOKEN  

```
>* install: [recommand] will install as system service. So it will autorun when system booting.
>* -d: daemon mode run once. When the worker process is found to exit unexpectedly, a new worker process will be automatically started
>* -node: Unique node name, unique identification
>* -token: See <console.openp2p.cn> "Profile"
>* -sharebandwidth: Provides bandwidth when used as a shared node, the default is 10mbps. If it is a large bandwidth of optical fiber, the larger the setting, the better the effect. 0 means not shared, the node is only used in a private P2P network. Do not join the shared P2P network, which also means that you CAN NOT use other people’s shared nodes
>* -loglevel: Need to view more debug logs, set 0; the default is 1

### Run in Docker container
We don't provide official docker image yet, you can run it in any container
```
nohup ./openp2p -d -node OFFICEPC1 -token TOKEN  &
# Since many docker images have been simplified, the install system service will fail, so the daemon mode is used to run in the background
```

## Connect
```
./openp2p -d -node HOMEPC123 -token TOKEN -appname OfficeWindowsRemote -peernode OFFICEPC1 -dstip 127.0.0.1 -dstport 3389 -srcport 23389
Create multiple P2PApp by config file
./openp2p -d    
```
>* -appname: This P2PApp name
>* -peernode: Target node name
>* -dstip: Target service address, default local 127.0.0.1
>* -dstport: Target service port, such as windows remote desktop 3389, Linux ssh 22
>* -protocol: Target service protocol tcp, udp

## Config file
Generally saved in the current directory, in installation mode it will be saved to `C:\Program Files\OpenP2P\config.json` or `/usr/local/openp2p/config.json`
If you want to modify the parameters, or configure multiple P2PApps, you can manually modify the configuration file

Configuration example
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

## Building a clean direct TCP tunnel

Some integrations (for example custom protocols or hardware devices) require that a direct TCP tunnel only carries the user payload. Enable the `disableTCPKeepalive` option to remove OpenP2P heartbeat frames and negotiate the raw stream upgrade between both peers.

1. **Upgrade both peers to a build that understands the option.** Older clients ignore the negotiation messages and will keep using the framed compatibility path even if one side enables the flag.
2. **Start both peers with the `--kk` flag.** Example:
   ```bash
   ./openp2p -d --kk -node NODE_A -token TOKEN_A
   ./openp2p -d --kk -node NODE_B -token TOKEN_B
   ```
   `--kk` toggles `disableTCPKeepalive=true` in memory so a direct TCP tunnel will not emit keepalive or heartbeat packets.
3. **Persist the preference in `config.json` if desired.** Add the flag at the top level:
   ```json
   {
     "disableTCPKeepalive": true,
     "network": { ... },
     "apps": [ ... ]
   }
   ```
   Launching via `./openp2p -d` will then automatically include the preference.
4. **Wait for the raw tunnel negotiation to finish.** When both peers accept the flag and a direct TCP tunnel is established, the log shows a line similar to `tunnel entering raw direct mode`. After that, application data (for example `GET /` requests) flows over the underlay TCP stream without any additional control frames.

> ⚠️ If either side leaves the flag disabled, the raw handshake does not complete within 5 seconds, or the tunnel falls back to relay mode, OpenP2P automatically returns to the framed transport so the connection stays healthy.

## Client update
```
# update local client
./openp2p update
# update remote client
curl --insecure 'https://api.openp2p.cn:27183/api/v1/device/YOUR-NODE-NAME/update?user=&password='
```

Windows system needs to set up firewall for this program, the program will automatically set the firewall, if the setting fails, the UDP punching will be affected.  
The default firewall configuration of Linux system (Ubuntu and CentOS7) will not have any effect, if not, you can try to turn off the firewall
```
systemctl stop firewalld.service
systemctl start firewalld.service
firewall-cmd --state
```

## Uninstall
```
./openp2p uninstall
# when already installed
# windows
C:\Program Files\OpenP2P\openp2p.exe uninstall
# linux,macos
sudo /usr/local/openp2p/openp2p uninstall
```

## Run with Docker
```
# Replace YOUR-TOKEN and YOUR-NODE-NAME with yours
docker run -d --net host --name openp2p-client -e OPENP2P_TOKEN=YOUR-TOKEN -e OPENP2P_NODE=YOUR-NODE-NAME  openp2pcn/openp2p-client:latest 
OR
docker run -d --net host --name openp2p-client  openp2pcn/openp2p-client:latest -token YOUR-TOKEN -node YOUR-NODE-NAME
```