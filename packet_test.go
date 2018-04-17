package main

import (
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var (
	srcMACTest         = net.HardwareAddr{0xFF, 0xAA, 0xFA, 0xAA, 0xFF, 0xAA}
	dstMACTest         = net.HardwareAddr{0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD}
	vlanIdentifierTest = uint16(30)
	srcIPv4Test        = net.IP{127, 0, 0, 1}
	dstIPv4Test        = net.IP{224, 0, 0, 251}
	srcIPv6Test        = net.ParseIP("::1")
	dstIPv6Test        = net.ParseIP("ff02::fb")
	srcUDPPortTest     = layers.UDPPort(5353)
	dstUDPPortTest     = layers.UDPPort(5353)
)

func createMockmDNSPacket(isIPv4 bool, isDNSQuery bool) []byte {
	var ethernetLayer, dot1QLayer, ipLayer, udpLayer, dnsLayer gopacket.SerializableLayer

	ethernetLayer = &layers.Ethernet{
		SrcMAC:       srcMACTest,
		DstMAC:       dstMACTest,
		EthernetType: layers.EthernetTypeDot1Q,
	}

	if isIPv4 {
		dot1QLayer = &layers.Dot1Q{
			VLANIdentifier: vlanIdentifierTest,
			Type:           layers.EthernetTypeIPv4,
		}

		ipLayer = &layers.IPv4{
			SrcIP:    srcIPv4Test,
			DstIP:    dstIPv4Test,
			Version:  4,
			Protocol: layers.IPProtocolUDP,
			Length:   146,
			IHL:      5,
			TOS:      0,
		}
	} else {
		dot1QLayer = &layers.Dot1Q{
			VLANIdentifier: vlanIdentifierTest,
			Type:           layers.EthernetTypeIPv6,
		}

		ipLayer = &layers.IPv6{
			SrcIP:      srcIPv6Test,
			DstIP:      dstIPv6Test,
			Version:    6,
			Length:     48,
			NextHeader: layers.IPProtocolUDP,
		}
	}

	udpLayer = &layers.UDP{
		SrcPort: srcUDPPortTest,
		DstPort: dstUDPPortTest,
	}

	if isDNSQuery {
		dnsLayer = &layers.DNS{
			Questions: []layers.DNSQuestion{layers.DNSQuestion{
				Name:  []byte("example.com"),
				Type:  layers.DNSTypeA,
				Class: layers.DNSClassIN,
			}},
			QDCount: 1,
		}
	} else {
		dnsLayer = &layers.DNS{
			Answers: []layers.DNSResourceRecord{layers.DNSResourceRecord{
				Name:  []byte("example.com"),
				Type:  layers.DNSTypeA,
				Class: layers.DNSClassIN,
				TTL:   1024,
				IP:    net.IP([]byte{1, 2, 3, 4}),
			}},
			ANCount: 1,
			QR:      true,
		}
	}

	buffer := gopacket.NewSerializeBuffer()
	gopacket.SerializeLayers(
		buffer,
		gopacket.SerializeOptions{},
		ethernetLayer,
		dot1QLayer,
		ipLayer,
		udpLayer,
		dnsLayer,
	)
	return buffer.Bytes()
}

func TestParseEthernetLayer(t *testing.T) {
	decoder := gopacket.DecodersByLayerName["Ethernet"]
	options := gopacket.DecodeOptions{Lazy: true}

	packet := gopacket.NewPacket(createMockmDNSPacket(true, true), decoder, options)

	expectedResult1, expectedResult2 := &srcMACTest, &dstMACTest
	computedResult1, computedResult2 := parseEthernetLayer(packet)
	if !reflect.DeepEqual(expectedResult1, computedResult1) || !reflect.DeepEqual(expectedResult2, computedResult2) {
		t.Error("Error in parseEthernetLayer()")
	}
}

func TestParseVLANTag(t *testing.T) {
	decoder := gopacket.DecodersByLayerName["Ethernet"]
	options := gopacket.DecodeOptions{Lazy: true}

	packet := gopacket.NewPacket(createMockmDNSPacket(true, true), decoder, options)

	expectedLayer := &layers.Dot1Q{
		VLANIdentifier: vlanIdentifierTest,
		Type:           layers.EthernetTypeIPv4,
	}
	expectedResult := &expectedLayer.VLANIdentifier
	computedResult := parseVLANTag(packet)
	if !reflect.DeepEqual(expectedResult, computedResult) {
		t.Error("Error in parseEthernetLayer()")
	}
}

func TestParseIPLayer(t *testing.T) {
	decoder := gopacket.DecodersByLayerName["Ethernet"]
	options := gopacket.DecodeOptions{Lazy: true}

	ipv4Packet := gopacket.NewPacket(createMockmDNSPacket(true, true), decoder, options)

	computedIsIPv6 := parseIPLayer(ipv4Packet)
	if computedIsIPv6 {
		t.Error("Error in parseIPLayer() for IPv4 addresses")
	}

	ipv6Packet := gopacket.NewPacket(createMockmDNSPacket(false, true), decoder, options)

	computedIsIPv6 = parseIPLayer(ipv6Packet)
	if !computedIsIPv6 {
		t.Error("Error in parseIPLayer() for IPv6 addresses")
	}
}

func TestParseDNSPayload(t *testing.T) {
	decoder := gopacket.DecodersByLayerName["Ethernet"]
	options := gopacket.DecodeOptions{Lazy: true}

	questionPacket := gopacket.NewPacket(createMockmDNSPacket(true, true), decoder, options)

	questionPacketPayload := parseUDPLayer(questionPacket)

	questionExpectedResult := true
	questionComputedResult := parseDNSPayload(questionPacketPayload)
	if !reflect.DeepEqual(questionExpectedResult, questionComputedResult) {
		t.Error("Error in parseDNSPayload() for DNS queries")
	}

	answerPacket := gopacket.NewPacket(createMockmDNSPacket(true, false), decoder, options)

	answerPacketPayload := parseUDPLayer(answerPacket)

	answerExpectedResult := false
	answerComputedResult := parseDNSPayload(answerPacketPayload)
	if !reflect.DeepEqual(answerExpectedResult, answerComputedResult) {
		t.Error("Error in parseDNSPayload() for DNS answers")
	}
}

type dataSource struct {
	packetSent bool
	data       []byte
}

func (dataSource *dataSource) ReadPacketData() (data []byte, ci gopacket.CaptureInfo, err error) {
	// Return one packet.
	// If a packet has already been returned in the past, return an EOF error
	// to end the reading of packets from this source.
	data = dataSource.data
	ci = gopacket.CaptureInfo{
		Timestamp:      time.Time{},
		CaptureLength:  len(data),
		Length:         ci.CaptureLength,
		InterfaceIndex: 0,
	}
	if !dataSource.packetSent {
		dataSource.packetSent = true
		return data, ci, nil
	}
	return nil, ci, io.EOF
}

func createMockPacketSource() (packetSource *gopacket.PacketSource, packet gopacket.Packet) {
	data := createMockmDNSPacket(true, true)
	dataSource := &dataSource{
		packetSent: false,
		data:       data,
	}
	decoder := gopacket.DecodersByLayerName["Ethernet"]
	packetSource = gopacket.NewPacketSource(dataSource, decoder)
	packet = gopacket.NewPacket(data, decoder, gopacket.DecodeOptions{Lazy: true})
	return
}

func areBonjourPacketsEqual(a, b bonjourPacket) (areEqual bool) {
	areEqual = (*a.vlanTag == *b.vlanTag) && (a.srcMAC.String() == b.srcMAC.String()) && (a.isDNSQuery == b.isDNSQuery)
	// While comparing Bonjour packets, we do not want to compare packets entirely.
	// In particular, packet.metadata may be slightly different, we do not need them to be the same.
	// So we only compare the layers part of the packets.
	areEqual = areEqual && reflect.DeepEqual(a.packet.Layers(), b.packet.Layers())
	return
}

func TestFilterBonjourPacketsLazily(t *testing.T) {
	mockPacketSource, packet := createMockPacketSource()
	packetChan := parsePacketsLazily(mockPacketSource)

	expectedResult := bonjourPacket{
		packet:     packet,
		vlanTag:    &vlanIdentifierTest,
		srcMAC:     &srcMACTest,
		isDNSQuery: true,
	}

	computedResult := <-packetChan
	if !areBonjourPacketsEqual(expectedResult, computedResult) {
		t.Error("Error in filterBonjourPacketsLazily()")
	}
}
