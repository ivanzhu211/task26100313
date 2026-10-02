package ledger

// Tx 表示一笔交易：发送方向接收方转移正整数金额，并携带发送方序号。
type Tx struct {
	From   string
	To     string
	Amount uint64
	Nonce  uint64
}

// Block 表示一个区块：标识、父块标识及有序交易列表。
type Block struct {
	ID     string
	Parent string
	Txs    []Tx
}

// Account 表示某地址在某一状态下的账户信息。
type Account struct {
	Balance uint64
	Nonce   uint64
}
