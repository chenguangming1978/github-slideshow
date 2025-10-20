Go Modbus TCP PLC client

说明
- 通过 Modbus TCP 访问 PLC（默认单元号 unit/slave ID = 1）。
- 读取和写入 40001、40002 两个保持寄存器（Holding Register）。
- 假设每个寄存器里打包了 16 个布尔量（位 bit0..bit15，bit0 为最低位 LSB）。
- 支持读取寄存器的 16 位值并以二进制展示；支持按位写入或整寄存器写掩码。

依赖
- Go 1.20+
- github.com/goburrow/modbus

构建
- 本目录是一个独立的 Go Module（嵌套在主项目中）。
- 可在本目录执行：
  - go build ./cmd/plcmodbus
  - 或 go run ./cmd/plcmodbus --help

环境变量
- MODBUS_ADDR: PLC 地址（host:port），默认 127.0.0.1:502
- MODBUS_UNIT: 单元号（1-247），默认 1
- MODBUS_TIMEOUT: 超时（例如 3s, 500ms），默认 3s

使用示例
- 读取两个寄存器：
  - go run ./cmd/plcmodbus read --addr 192.168.0.10:502 --unit 1
- 将 40001 的 bit3 置 1：
  - go run ./cmd/plcmodbus write --reg 40001 --bit 3 --value 1 --addr 192.168.0.10:502
- 将 40002 整个寄存器写为 0x00FF：
  - go run ./cmd/plcmodbus write --reg 40002 --mask 0x00FF --addr 192.168.0.10:502

注意
- 地址换算：库使用从 0 开始的地址，40001 -> 0，40002 -> 1。
- 写入按位时，程序会先读当前寄存器值，修改相应位，再以写单寄存器的方式写回。
- 位序说明：bit0 是最低位（LSB），bit15 是最高位（MSB）。

English summary
- Modbus TCP client for reading/writing 16 boolean flags packed in holding registers 40001 and 40002.
- Provides read (dump both registers) and write (per-bit or full 16-bit mask) commands.
- Configure address/unit/timeout via flags or environment variables.
