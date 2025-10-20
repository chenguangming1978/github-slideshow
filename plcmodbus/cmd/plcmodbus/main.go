package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goburrow/modbus"
)

// PLC wraps a Modbus TCP client for convenient register bit operations.
type PLC struct {
	handler *modbus.TCPClientHandler
	client  modbus.Client
}

// NewPLC creates and connects a Modbus TCP client.
func NewPLC(address string, unitID byte, timeout time.Duration) (*PLC, error) {
	h := modbus.NewTCPClientHandler(address)
	h.Timeout = timeout
	h.SlaveId = unitID
	// Keep logs quiet by default; uncomment for debugging
	// h.Logger = log.New(os.Stdout, "modbus ", log.LstdFlags)
	if err := h.Connect(); err != nil {
		return nil, err
	}
	return &PLC{handler: h, client: modbus.NewClient(h)}, nil
}

func (p *PLC) Close() {
	if p != nil && p.handler != nil {
		_ = p.handler.Close()
	}
}

// regToAddr converts Modbus 4x register number (e.g., 40001) to zero-based address for library calls.
func regToAddr(reg int) (uint16, error) {
	if reg < 40001 {
		return 0, fmt.Errorf("unsupported register: %d (expect 40001 or above)", reg)
	}
	addr := reg - 40001
	if addr < 0 || addr > 0xFFFF {
		return 0, fmt.Errorf("register out of range: %d", reg)
	}
	return uint16(addr), nil
}

// ReadRegister returns the 16-bit value and its bits for a holding register.
func (p *PLC) ReadRegister(reg int) (uint16, [16]bool, error) {
	addr, err := regToAddr(reg)
	if err != nil {
		return 0, [16]bool{}, err
	}
	data, err := p.client.ReadHoldingRegisters(addr, 1)
	if err != nil {
		return 0, [16]bool{}, err
	}
	if len(data) != 2 {
		return 0, [16]bool{}, fmt.Errorf("unexpected response length: %d", len(data))
	}
	val := binary.BigEndian.Uint16(data)
	var bits [16]bool
	for i := 0; i < 16; i++ {
		bits[i] = ((val >> uint(i)) & 1) == 1
	}
	return val, bits, nil
}

// WriteRegister writes a full 16-bit value to the holding register.
func (p *PLC) WriteRegister(reg int, value uint16) error {
	addr, err := regToAddr(reg)
	if err != nil {
		return err
	}
	_, err = p.client.WriteSingleRegister(addr, value)
	return err
}

// WriteRegisterBit sets a specific bit (0-15) in the holding register.
func (p *PLC) WriteRegisterBit(reg int, bit int, value bool) (uint16, error) {
	if bit < 0 || bit > 15 {
		return 0, fmt.Errorf("bit index out of range: %d (expect 0-15)", bit)
	}
	cur, _, err := p.ReadRegister(reg)
	if err != nil {
		return 0, err
	}
	mask := uint16(1) << uint(bit)
	var newVal uint16
	if value {
		newVal = cur | mask
	} else {
		newVal = cur &^ mask
	}
	if newVal == cur {
		return newVal, nil // no change needed
	}
	return newVal, p.WriteRegister(reg, newVal)
}

func printBits(reg int, value uint16) {
	bits := make([]string, 16)
	for i := 15; i >= 0; i-- { // MSB first for readability
		if ((value >> uint(i)) & 1) == 1 {
			bits[15-i] = "1"
		} else {
			bits[15-i] = "0"
		}
	}
	fmt.Printf("Register %d: 0x%04X b=%016b\n", reg, value, value)
	fmt.Printf("Bits [15..0]: %s\n", strings.Join(bits, " "))
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	// Global flags
	var (
		addr    string
		unitStr string
		timeout time.Duration
	)
	global := flag.NewFlagSet("global", flag.ContinueOnError)
	global.SetOutput(os.Stdout)
	global.StringVar(&addr, "addr", envOr("MODBUS_ADDR", "127.0.0.1:502"), "Modbus TCP address host:port")
	global.StringVar(&unitStr, "unit", envOr("MODBUS_UNIT", "1"), "Modbus unit/slave ID (1-247)")
	global.DurationVar(&timeout, "timeout", parseDurationEnv("MODBUS_TIMEOUT", 3*time.Second), "TCP timeout (e.g. 2s, 500ms)")

	subcmd := os.Args[1]
	switch subcmd {
	case "read":
		_ = global.Parse(os.Args[2:])
		unit, err := parseUnit(unitStr)
		if err != nil {
			log.Fatalf("invalid unit: %v", err)
		}
		plc, err := NewPLC(addr, unit, timeout)
		if err != nil {
			log.Fatalf("connect failed: %v", err)
		}
		defer plc.Close()

		for _, reg := range []int{40001, 40002} {
			val, _, err := plc.ReadRegister(reg)
			if err != nil {
				log.Fatalf("read register %d: %v", reg, err)
			}
			printBits(reg, val)
		}
	case "write":
		writeFlags := flag.NewFlagSet("write", flag.ContinueOnError)
		writeFlags.SetOutput(os.Stdout)
		writeFlags.StringVar(&addr, "addr", envOr("MODBUS_ADDR", "127.0.0.1:502"), "Modbus TCP address host:port")
		writeFlags.StringVar(&unitStr, "unit", envOr("MODBUS_UNIT", "1"), "Modbus unit/slave ID (1-247)")
		writeFlags.DurationVar(&timeout, "timeout", parseDurationEnv("MODBUS_TIMEOUT", 3*time.Second), "TCP timeout (e.g. 2s, 500ms)")

		regNum := writeFlags.Int("reg", 40001, "Target register (40001 or 40002)")
		bitIdx := writeFlags.Int("bit", -1, "Bit index to set (0-15). If omitted, use --mask instead.")
		bitVal := writeFlags.String("value", "", "Bit value to write when using --bit (0 or 1, true or false)")
		maskStr := writeFlags.String("mask", "", "Full 16-bit value to write to the register (e.g., 0x00FF, 255, 0b1010)")

		if err := writeFlags.Parse(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		unit, err := parseUnit(unitStr)
		if err != nil {
			log.Fatalf("invalid unit: %v", err)
		}
		if *regNum != 40001 && *regNum != 40002 {
			log.Fatalf("invalid --reg: %d (expected 40001 or 40002)", *regNum)
		}

		plc, err := NewPLC(addr, unit, timeout)
		if err != nil {
			log.Fatalf("connect failed: %v", err)
		}
		defer plc.Close()

		if *maskStr != "" {
			// Write full register
			maskVal, err := parseUint16(*maskStr)
			if err != nil {
				log.Fatalf("invalid --mask: %v", err)
			}
			if err := plc.WriteRegister(*regNum, maskVal); err != nil {
				log.Fatalf("write register %d: %v", *regNum, err)
			}
			fmt.Printf("Wrote register %d = 0x%04X\n", *regNum, maskVal)
			val, _, _ := plc.ReadRegister(*regNum)
			printBits(*regNum, val)
			return
		}

		// else using bit mode
		if *bitIdx < 0 || *bitIdx > 15 {
			log.Fatalf("--bit is required (0-15) when --mask is not provided")
		}
		if *bitVal == "" {
			log.Fatalf("--value is required when using --bit")
		}
		vbool, err := parseBool(*bitVal)
		if err != nil {
			log.Fatalf("invalid --value: %v", err)
		}
		newVal, err := plc.WriteRegisterBit(*regNum, *bitIdx, vbool)
		if err != nil {
			log.Fatalf("write bit failed: %v", err)
		}
		fmt.Printf("Updated register %d bit %d -> %v (new=0x%04X)\n", *regNum, *bitIdx, vbool, newVal)
		val, _, _ := plc.ReadRegister(*regNum)
		printBits(*regNum, val)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("PLC Modbus TCP client for reading/writing 16 bools in holding registers 40001 and 40002")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  plcmodbus read [--addr host:port] [--unit 1] [--timeout 3s]")
	fmt.Println("  plcmodbus write --reg {40001|40002} --bit N --value {0|1} [--addr host:port] [--unit 1] [--timeout 3s]")
	fmt.Println("  plcmodbus write --reg {40001|40002} --mask <uint16> [--addr host:port] [--unit 1] [--timeout 3s]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  plcmodbus read --addr 192.168.0.10:502 --unit 1")
	fmt.Println("  plcmodbus write --reg 40001 --bit 3 --value 1 --addr 192.168.0.10:502")
	fmt.Println("  plcmodbus write --reg 40002 --mask 0x00FF --addr 192.168.0.10:502")
}

func parseUnit(s string) (byte, error) {
	u64, err := strconv.ParseUint(strings.TrimSpace(s), 10, 8)
	if err != nil {
		return 0, err
	}
	u := byte(u64)
	if u < 1 || u > 247 {
		return 0, fmt.Errorf("unit out of range: %d (expect 1-247)", u)
	}
	return u, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDurationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseBool(s string) (bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	default:
		return false, errors.New("expected boolean value (0/1/true/false)")
	}
}

// parseUint16 parses decimal (e.g., 255), hex (e.g., 0x00FF), or binary (e.g., 0b1010) to uint16.
func parseUint16(s string) (uint16, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	base := 10
	if strings.HasPrefix(s, "0x") {
		base = 16
		s = s[2:]
	} else if strings.HasPrefix(s, "0b") {
		base = 2
		s = s[2:]
	}
	u, err := strconv.ParseUint(s, base, 16)
	if err != nil {
		return 0, err
	}
	return uint16(u), nil
}
