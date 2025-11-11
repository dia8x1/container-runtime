package container

import (
	"fmt"
	"runtime"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func setupContainerNetwork(containerNetns string, vethName string, vethPeerName string, containerIP string) error {
	br, err := netlink.LinkByName("cr0") // cr0 bridge는 install.sh을 통해 미리 구현된 것을 사용
	if err != nil {
		return fmt.Errorf("failed to get bridge cr0: %w", err)
	}

	if err := netlink.LinkSetUp(br); err != nil {
		return fmt.Errorf("failed to set bridge cr0 up: %w", err)
	}

	// veth 쌍 생성 (하나는 bridge, 하나는 container에 연결)
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: vethName,
		},
		PeerName: vethPeerName,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("failed to create veth pair: %w", err)
	}

	vethLink, err := netlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to get veth link: %w", err)
	}

	if err := netlink.LinkSetMaster(vethLink, br); err != nil {
		netlink.LinkDel(vethLink)
		return fmt.Errorf("failed to attach veth to bridge: %w", err)
	}
	if err := netlink.LinkSetUp(vethLink); err != nil {
		netlink.LinkDel(vethLink)
		return fmt.Errorf("failed to set veth up: %w", err)
	}

	peer, err := netlink.LinkByName(vethPeerName)
	if err != nil {
		netlink.LinkDel(vethLink)
		return fmt.Errorf("failed to get veth peer: %w", err)
	}

	netnsHandle, err := netns.GetFromPath(containerNetns)
	if err != nil {
		netlink.LinkDel(vethLink)
		return fmt.Errorf("failed to get netns handle: %w", err)
	}
	defer netnsHandle.Close()

	if err := netlink.LinkSetNsFd(peer, int(netnsHandle)); err != nil {
		netlink.LinkDel(vethLink)
		return fmt.Errorf("failed to move veth peer to netns: %w", err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNetns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get original netns: %w", err)
	}
	defer origNetns.Close()

	if err := netns.Set(netnsHandle); err != nil {
		return fmt.Errorf("failed to switch to container netns: %w", err)
	}
	defer netns.Set(origNetns)

	peer, err = netlink.LinkByName(vethPeerName)
	if err != nil {
		return fmt.Errorf("failed to get peer in container netns: %w", err)
	}

	if err := netlink.LinkSetName(peer, "eth0"); err != nil {
		return fmt.Errorf("failed to rename peer to eth0: %w", err)
	}

	peer, err = netlink.LinkByName("eth0")
	if err != nil {
		return fmt.Errorf("failed to get renamed eth0 interface: %w", err)
	}

	addr, err := netlink.ParseAddr(containerIP)
	if err != nil {
		return fmt.Errorf("failed to parse IP address: %w", err)
	}

	if err := netlink.AddrAdd(peer, addr); err != nil {
		return fmt.Errorf("failed to add IP address: %w", err)
	}

	if err := netlink.LinkSetUp(peer); err != nil {
		return fmt.Errorf("failed to set peer up: %w", err)
	}

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to get loopback interface: %w", err)
	}

	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("failed to set loopback up: %w", err)
	}

	return nil
}