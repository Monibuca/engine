package util

import "net"

// IsLANAddr 检测 IP 地址字符串是否是内网地址
func IsLANAddr(ip string) (bool, string) {
	return IsLANIp(net.ParseIP(ip))
}

// IsLANIp 检测 IP 地址是否是内网地址
// 通过直接对比ip段范围效率更高
func IsLANIp(ip net.IP) (bool, string) {
	if ip.IsLoopback() {
		return true, "127.0.0.0/24"
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return false, ""
	}
	if ip4[0] == 10 { // 10.0.0.0/8
		return true, "10.0.0.0/8"
	} else if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 { // 172.16.0.0/12
		return true, "172.16.0.0/12"
	} else if ip4[0] == 169 && ip4[1] == 254 { // 169.254.0.0/16
		return true, "169.254.0.0/16"
	} else if ip4[0] == 192 && ip4[1] == 168 { //192.168.0.0/16
		return true, "192.168.0.0/16"
	}
	return false, ""
}
