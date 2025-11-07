package main

import "strconv"

// DoraSlotData represents fields returned by the original Dora upstream.
type DoraSlotData struct {
	AttestationsCount          uint64  `json:"attestationscount"`
	AttesterSlashingsCount     uint64  `json:"attesterslashingscount"`
	BlockRoot                  string  `json:"blockroot"`
	DepositsCount              uint64  `json:"depositscount"`
	Epoch                      uint64  `json:"epoch"`
	ExecBaseFeePerGas          uint64  `json:"exec_base_fee_per_gas"`
	ExecBlockHash              string  `json:"exec_block_hash"`
	ExecBlockNumber            uint64  `json:"exec_block_number"`
	ExecExtraData              string  `json:"exec_extra_data"`
	ExecFeeRecipient           string  `json:"exec_fee_recipient"`
	ExecGasLimit               uint64  `json:"exec_gas_limit"`
	ExecGasUsed                uint64  `json:"exec_gas_used"`
	ExecTransactionsCount      uint64  `json:"exec_transactions_count"`
	Graffiti                   string  `json:"graffiti"`
	GraffitiText               string  `json:"graffiti_text"`
	ParentRoot                 string  `json:"parentroot"`
	Proposer                   uint64  `json:"proposer"`
	ProposerSlashingsCount     uint64  `json:"proposerslashingscount"`
	Slot                       uint64  `json:"slot"`
	StateRoot                  string  `json:"stateroot"`
	Status                     string  `json:"status"`
	SyncAggregateParticipation float64 `json:"syncaggregate_participation"`
	VoluntaryExitsCount        uint64  `json:"voluntaryexitscount"`
	WithdrawalCount            uint64  `json:"withdrawalcount"`
	BlobCount                  uint64  `json:"blob_count"`
}

// BeaconMissingFields represents fields that Beacon has but Dora does not.
// JSON tags match Beacon's field names.
type BeaconMissingFields struct {
	Eth1dataBlockHash      string `json:"eth1data_blockhash"`
	Eth1dataDepositCount   uint64 `json:"eth1data_depositcount"`
	Eth1dataDepositRoot    string `json:"eth1data_depositroot"`
	ExecLogsBloom          string `json:"exec_logs_bloom"`
	ExecParentHash         string `json:"exec_parent_hash"`
	ExecRandom             string `json:"exec_random"`
	ExecReceiptsRoot       string `json:"exec_receipts_root"`
	ExecStateRoot          string `json:"exec_state_root"`
	ExecTimestamp          uint64 `json:"exec_timestamp"`
	Randaoreveal           string `json:"randaoreveal"`
	Signature              string `json:"signature"`
	SyncaggregateBits      string `json:"syncaggregate_bits"`
	SyncaggregateSignature string `json:"syncaggregate_signature"`
}

// SlotResponse is the flattened response composed of DoraSlotData and BeaconMissingFields.
type SlotResponse struct {
	DoraSlotData
	BeaconMissingFields
}

func buildSlotResponseFromMap(m map[string]interface{}) SlotResponse {
	return SlotResponse{
		DoraSlotData: DoraSlotData{
			AttestationsCount:          asUint(m["attestationscount"]),
			AttesterSlashingsCount:     asUint(m["attesterslashingscount"]),
			BlockRoot:                  asString(m["blockroot"]),
			DepositsCount:              asUint(m["depositscount"]),
			Epoch:                      asUint(m["epoch"]),
			ExecBaseFeePerGas:          asUint(m["exec_base_fee_per_gas"]),
			ExecBlockHash:              asString(m["exec_block_hash"]),
			ExecBlockNumber:            asUint(m["exec_block_number"]),
			ExecExtraData:              asString(m["exec_extra_data"]),
			ExecFeeRecipient:           asString(m["exec_fee_recipient"]),
			ExecGasLimit:               asUint(m["exec_gas_limit"]),
			ExecGasUsed:                asUint(m["exec_gas_used"]),
			ExecTransactionsCount:      asUint(m["exec_transactions_count"]),
			Graffiti:                   asString(m["graffiti"]),
			GraffitiText:               asString(m["graffiti_text"]),
			ParentRoot:                 asString(m["parentroot"]),
			Proposer:                   asUint(m["proposer"]),
			ProposerSlashingsCount:     asUint(m["proposerslashingscount"]),
			Slot:                       asUint(m["slot"]),
			StateRoot:                  asString(m["stateroot"]),
			Status:                     asString(m["status"]),
			SyncAggregateParticipation: asFloat(m["syncaggregate_participation"]),
			VoluntaryExitsCount:        asUint(m["voluntaryexitscount"]),
			WithdrawalCount:            asUint(m["withdrawalcount"]),
			BlobCount:                  asUint(m["blob_count"]),
		},
		BeaconMissingFields: BeaconMissingFields{
			Eth1dataBlockHash:      asString(m["eth1data_blockhash"]),
			Eth1dataDepositCount:   asUint(m["eth1data_depositcount"]),
			Eth1dataDepositRoot:    asString(m["eth1data_depositroot"]),
			ExecLogsBloom:          asString(m["exec_logs_bloom"]),
			ExecParentHash:         asString(m["exec_parent_hash"]),
			ExecRandom:             asString(m["exec_random"]),
			ExecReceiptsRoot:       asString(m["exec_receipts_root"]),
			ExecStateRoot:          asString(m["exec_state_root"]),
			ExecTimestamp:          asUint(m["exec_timestamp"]),
			Randaoreveal:           asString(m["randaoreveal"]),
			Signature:              asString(m["signature"]),
			SyncaggregateBits:      asString(m["syncaggregate_bits"]),
			SyncaggregateSignature: asString(m["syncaggregate_signature"]),
		},
	}
}

func asUint(v interface{}) uint64 {
	switch t := v.(type) {
	case float64:
		return uint64(t)
	case string:
		if t == "" {
			return 0
		}
		n, err := strconv.ParseUint(t, 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func asFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		if t == "" {
			return 0
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
