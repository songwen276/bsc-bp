// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethapi

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/pair"
	"github.com/ethereum/go-ethereum/pair/pairtypes"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/accounts/scwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/gasestimator"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/miguelmota/go-solidity-sha3"
	"github.com/tyler-smith/go-bip39"
)

// max is a helper function which returns the larger of the two given integers.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

const UnHealthyTimeout = 5 * time.Second

// estimateGasErrorRatio is the amount of overestimation eth_estimateGas is
// allowed to produce in order to speed up calculations.
const estimateGasErrorRatio = 0.015

var errBlobTxNotSupported = errors.New("signing blob transactions not supported")

// EthereumAPI provides an API to access Ethereum related information.
type EthereumAPI struct {
	b Backend
}

// NewEthereumAPI creates a new Ethereum protocol API.
func NewEthereumAPI(b Backend) *EthereumAPI {
	return &EthereumAPI{b}
}

// GasPrice returns a suggestion for a gas price for legacy transactions.
func (s *EthereumAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	tipcap, err := s.b.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	if head := s.b.CurrentHeader(); head.BaseFee != nil {
		tipcap.Add(tipcap, head.BaseFee)
	}
	return (*hexutil.Big)(tipcap), err
}

// MaxPriorityFeePerGas returns a suggestion for a gas tip cap for dynamic fee transactions.
func (s *EthereumAPI) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	tipcap, err := s.b.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(tipcap), err
}

type feeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

// FeeHistory returns the fee market history.
func (s *EthereumAPI) FeeHistory(ctx context.Context, blockCount math.HexOrDecimal64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*feeHistoryResult, error) {
	log.Info("执行EthereumAPI.FeeHistory方法")
	oldest, reward, baseFee, gasUsed, err := s.b.FeeHistory(ctx, uint64(blockCount), lastBlock, rewardPercentiles)
	if err != nil {
		return nil, err
	}
	results := &feeHistoryResult{
		OldestBlock:  (*hexutil.Big)(oldest),
		GasUsedRatio: gasUsed,
	}
	if reward != nil {
		results.Reward = make([][]*hexutil.Big, len(reward))
		for i, w := range reward {
			results.Reward[i] = make([]*hexutil.Big, len(w))
			for j, v := range w {
				results.Reward[i][j] = (*hexutil.Big)(v)
			}
		}
	}
	if baseFee != nil {
		results.BaseFee = make([]*hexutil.Big, len(baseFee))
		for i, v := range baseFee {
			results.BaseFee[i] = (*hexutil.Big)(v)
		}
	}
	return results, nil
}

// Syncing returns false in case the node is currently not syncing with the network. It can be up-to-date or has not
// yet received the latest block headers from its pears. In case it is synchronizing:
// - startingBlock: block number this node started to synchronize from
// - currentBlock:  block number this node is currently importing
// - highestBlock:  block number of the highest block header this node has received from peers
// - pulledStates:  number of state entries processed until now
// - knownStates:   number of known state entries that still need to be pulled
func (s *EthereumAPI) Syncing() (interface{}, error) {
	progress := s.b.SyncProgress()

	// Return not syncing if the synchronisation already completed
	if progress.Done() {
		return false, nil
	}
	// Otherwise gather the block sync stats
	return map[string]interface{}{
		"startingBlock":          hexutil.Uint64(progress.StartingBlock),
		"currentBlock":           hexutil.Uint64(progress.CurrentBlock),
		"highestBlock":           hexutil.Uint64(progress.HighestBlock),
		"syncedAccounts":         hexutil.Uint64(progress.SyncedAccounts),
		"syncedAccountBytes":     hexutil.Uint64(progress.SyncedAccountBytes),
		"syncedBytecodes":        hexutil.Uint64(progress.SyncedBytecodes),
		"syncedBytecodeBytes":    hexutil.Uint64(progress.SyncedBytecodeBytes),
		"syncedStorage":          hexutil.Uint64(progress.SyncedStorage),
		"syncedStorageBytes":     hexutil.Uint64(progress.SyncedStorageBytes),
		"healedTrienodes":        hexutil.Uint64(progress.HealedTrienodes),
		"healedTrienodeBytes":    hexutil.Uint64(progress.HealedTrienodeBytes),
		"healedBytecodes":        hexutil.Uint64(progress.HealedBytecodes),
		"healedBytecodeBytes":    hexutil.Uint64(progress.HealedBytecodeBytes),
		"healingTrienodes":       hexutil.Uint64(progress.HealingTrienodes),
		"healingBytecode":        hexutil.Uint64(progress.HealingBytecode),
		"txIndexFinishedBlocks":  hexutil.Uint64(progress.TxIndexFinishedBlocks),
		"txIndexRemainingBlocks": hexutil.Uint64(progress.TxIndexRemainingBlocks),
	}, nil
}

// TxPoolAPI offers and API for the transaction pool. It only operates on data that is non-confidential.
type TxPoolAPI struct {
	b Backend
}

// NewTxPoolAPI creates a new tx pool service that gives information about the transaction pool.
func NewTxPoolAPI(b Backend) *TxPoolAPI {
	return &TxPoolAPI{b}
}

// Content returns the transactions contained within the transaction pool.
func (s *TxPoolAPI) Content() map[string]map[string]map[string]*RPCTransaction {
	content := map[string]map[string]map[string]*RPCTransaction{
		"pending": make(map[string]map[string]*RPCTransaction),
		"queued":  make(map[string]map[string]*RPCTransaction),
	}
	pending, queue := s.b.TxPoolContent()
	curHeader := s.b.CurrentHeader()
	// Flatten the pending transactions
	for account, txs := range pending {
		dump := make(map[string]*RPCTransaction)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = NewRPCPendingTransaction(tx, curHeader, s.b.ChainConfig())
		}
		content["pending"][account.Hex()] = dump
	}
	// Flatten the queued transactions
	for account, txs := range queue {
		dump := make(map[string]*RPCTransaction)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = NewRPCPendingTransaction(tx, curHeader, s.b.ChainConfig())
		}
		content["queued"][account.Hex()] = dump
	}
	return content
}

// ContentFrom returns the transactions contained within the transaction pool.
func (s *TxPoolAPI) ContentFrom(addr common.Address) map[string]map[string]*RPCTransaction {
	content := make(map[string]map[string]*RPCTransaction, 2)
	pending, queue := s.b.TxPoolContentFrom(addr)
	curHeader := s.b.CurrentHeader()

	// Build the pending transactions
	dump := make(map[string]*RPCTransaction, len(pending))
	for _, tx := range pending {
		dump[fmt.Sprintf("%d", tx.Nonce())] = NewRPCPendingTransaction(tx, curHeader, s.b.ChainConfig())
	}
	content["pending"] = dump

	// Build the queued transactions
	dump = make(map[string]*RPCTransaction, len(queue))
	for _, tx := range queue {
		dump[fmt.Sprintf("%d", tx.Nonce())] = NewRPCPendingTransaction(tx, curHeader, s.b.ChainConfig())
	}
	content["queued"] = dump

	return content
}

// Status returns the number of pending and queued transaction in the pool.
func (s *TxPoolAPI) Status() map[string]hexutil.Uint {
	pending, queue := s.b.Stats()
	return map[string]hexutil.Uint{
		"pending": hexutil.Uint(pending),
		"queued":  hexutil.Uint(queue),
	}
}

// Inspect retrieves the content of the transaction pool and flattens it into an
// easily inspectable list.
func (s *TxPoolAPI) Inspect() map[string]map[string]map[string]string {
	content := map[string]map[string]map[string]string{
		"pending": make(map[string]map[string]string),
		"queued":  make(map[string]map[string]string),
	}
	pending, queue := s.b.TxPoolContent()

	// Define a formatter to flatten a transaction into a string
	var format = func(tx *types.Transaction) string {
		if to := tx.To(); to != nil {
			return fmt.Sprintf("%s: %v wei + %v gas × %v wei", tx.To().Hex(), tx.Value(), tx.Gas(), tx.GasPrice())
		}
		return fmt.Sprintf("contract creation: %v wei + %v gas × %v wei", tx.Value(), tx.Gas(), tx.GasPrice())
	}
	// Flatten the pending transactions
	for account, txs := range pending {
		dump := make(map[string]string)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = format(tx)
		}
		content["pending"][account.Hex()] = dump
	}
	// Flatten the queued transactions
	for account, txs := range queue {
		dump := make(map[string]string)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = format(tx)
		}
		content["queued"][account.Hex()] = dump
	}
	return content
}

// EthereumAccountAPI provides an API to access accounts managed by this node.
// It offers only methods that can retrieve accounts.
type EthereumAccountAPI struct {
	am *accounts.Manager
}

// NewEthereumAccountAPI creates a new EthereumAccountAPI.
func NewEthereumAccountAPI(am *accounts.Manager) *EthereumAccountAPI {
	return &EthereumAccountAPI{am: am}
}

// Accounts returns the collection of accounts this node manages.
func (s *EthereumAccountAPI) Accounts() []common.Address {
	return s.am.Accounts()
}

// PersonalAccountAPI provides an API to access accounts managed by this node.
// It offers methods to create, (un)lock en list accounts. Some methods accept
// passwords and are therefore considered private by default.
type PersonalAccountAPI struct {
	am        *accounts.Manager
	nonceLock *AddrLocker
	b         Backend
}

// NewPersonalAccountAPI creates a new PersonalAccountAPI.
func NewPersonalAccountAPI(b Backend, nonceLock *AddrLocker) *PersonalAccountAPI {
	return &PersonalAccountAPI{
		am:        b.AccountManager(),
		nonceLock: nonceLock,
		b:         b,
	}
}

// ListAccounts will return a list of addresses for accounts this node manages.
func (s *PersonalAccountAPI) ListAccounts() []common.Address {
	return s.am.Accounts()
}

// rawWallet is a JSON representation of an accounts.Wallet interface, with its
// data contents extracted into plain fields.
type rawWallet struct {
	URL      string             `json:"url"`
	Status   string             `json:"status"`
	Failure  string             `json:"failure,omitempty"`
	Accounts []accounts.Account `json:"accounts,omitempty"`
}

// ListWallets will return a list of wallets this node manages.
func (s *PersonalAccountAPI) ListWallets() []rawWallet {
	wallets := make([]rawWallet, 0) // return [] instead of nil if empty
	for _, wallet := range s.am.Wallets() {
		status, failure := wallet.Status()

		raw := rawWallet{
			URL:      wallet.URL().String(),
			Status:   status,
			Accounts: wallet.Accounts(),
		}
		if failure != nil {
			raw.Failure = failure.Error()
		}
		wallets = append(wallets, raw)
	}
	return wallets
}

// OpenWallet initiates a hardware wallet opening procedure, establishing a USB
// connection and attempting to authenticate via the provided passphrase. Note,
// the method may return an extra challenge requiring a second open (e.g. the
// Trezor PIN matrix challenge).
func (s *PersonalAccountAPI) OpenWallet(url string, passphrase *string) error {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return err
	}
	pass := ""
	if passphrase != nil {
		pass = *passphrase
	}
	return wallet.Open(pass)
}

// DeriveAccount requests an HD wallet to derive a new account, optionally pinning
// it for later reuse.
func (s *PersonalAccountAPI) DeriveAccount(url string, path string, pin *bool) (accounts.Account, error) {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return accounts.Account{}, err
	}
	derivPath, err := accounts.ParseDerivationPath(path)
	if err != nil {
		return accounts.Account{}, err
	}
	if pin == nil {
		pin = new(bool)
	}
	return wallet.Derive(derivPath, *pin)
}

// NewAccount will create a new account and returns the address for the new account.
func (s *PersonalAccountAPI) NewAccount(password string) (common.AddressEIP55, error) {
	ks, err := fetchKeystore(s.am)
	if err != nil {
		return common.AddressEIP55{}, err
	}
	acc, err := ks.NewAccount(password)
	if err == nil {
		addrEIP55 := common.AddressEIP55(acc.Address)
		log.Info("Your new key was generated", "address", addrEIP55.String())
		log.Warn("Please backup your key file!", "path", acc.URL.Path)
		log.Warn("Please remember your password!")
		return addrEIP55, nil
	}
	return common.AddressEIP55{}, err
}

// fetchKeystore retrieves the encrypted keystore from the account manager.
func fetchKeystore(am *accounts.Manager) (*keystore.KeyStore, error) {
	if ks := am.Backends(keystore.KeyStoreType); len(ks) > 0 {
		return ks[0].(*keystore.KeyStore), nil
	}
	return nil, errors.New("local keystore not used")
}

// ImportRawKey stores the given hex encoded ECDSA key into the key directory,
// encrypting it with the passphrase.
func (s *PersonalAccountAPI) ImportRawKey(privkey string, password string) (common.Address, error) {
	key, err := crypto.HexToECDSA(privkey)
	if err != nil {
		return common.Address{}, err
	}
	ks, err := fetchKeystore(s.am)
	if err != nil {
		return common.Address{}, err
	}
	acc, err := ks.ImportECDSA(key, password)
	return acc.Address, err
}

// UnlockAccount will unlock the account associated with the given address with
// the given password for duration seconds. If duration is nil it will use a
// default of 300 seconds. It returns an indication if the account was unlocked.
func (s *PersonalAccountAPI) UnlockAccount(ctx context.Context, addr common.Address, password string, duration *uint64) (bool, error) {
	// When the API is exposed by external RPC(http, ws etc), unless the user
	// explicitly specifies to allow the insecure account unlocking, otherwise
	// it is disabled.
	if s.b.ExtRPCEnabled() && !s.b.AccountManager().Config().InsecureUnlockAllowed {
		return false, errors.New("account unlock with HTTP access is forbidden")
	}

	const max = uint64(time.Duration(math.MaxInt64) / time.Second)
	var d time.Duration
	if duration == nil {
		d = 300 * time.Second
	} else if *duration > max {
		return false, errors.New("unlock duration too large")
	} else {
		d = time.Duration(*duration) * time.Second
	}
	ks, err := fetchKeystore(s.am)
	if err != nil {
		return false, err
	}
	err = ks.TimedUnlock(accounts.Account{Address: addr}, password, d)
	if err != nil {
		log.Warn("Failed account unlock attempt", "address", addr, "err", err)
	}
	return err == nil, err
}

// LockAccount will lock the account associated with the given address when it's unlocked.
func (s *PersonalAccountAPI) LockAccount(addr common.Address) bool {
	if ks, err := fetchKeystore(s.am); err == nil {
		return ks.Lock(addr) == nil
	}
	return false
}

// signTransaction sets defaults and signs the given transaction
// NOTE: the caller needs to ensure that the nonceLock is held, if applicable,
// and release it after the transaction has been submitted to the tx pool
func (s *PersonalAccountAPI) signTransaction(ctx context.Context, args *TransactionArgs, passwd string) (*types.Transaction, error) {
	// Look up the wallet containing the requested signer
	account := accounts.Account{Address: args.from()}
	wallet, err := s.am.Find(account)
	if err != nil {
		return nil, err
	}
	// Set some sanity defaults and terminate on failure
	if err := args.setDefaults(ctx, s.b, false); err != nil {
		return nil, err
	}
	// Assemble the transaction and sign with the wallet
	tx := args.toTransaction()

	return wallet.SignTxWithPassphrase(account, passwd, tx, s.b.ChainConfig().ChainID)
}

// SendTransaction will create a transaction from the given arguments and
// tries to sign it with the key associated with args.From. If the given
// passwd isn't able to decrypt the key it fails.
func (s *PersonalAccountAPI) SendTransaction(ctx context.Context, args TransactionArgs, passwd string) (common.Hash, error) {
	if args.Nonce == nil {
		// Hold the mutex around signing to prevent concurrent assignment of
		// the same nonce to multiple accounts.
		s.nonceLock.LockAddr(args.from())
		defer s.nonceLock.UnlockAddr(args.from())
	}
	if args.IsEIP4844() {
		return common.Hash{}, errBlobTxNotSupported
	}
	signed, err := s.signTransaction(ctx, &args, passwd)
	if err != nil {
		log.Warn("Failed transaction send attempt", "from", args.from(), "to", args.To, "value", args.Value.ToInt(), "err", err)
		return common.Hash{}, err
	}
	return SubmitTransaction(ctx, s.b, signed)
}

// SignTransaction will create a transaction from the given arguments and
// tries to sign it with the key associated with args.From. If the given passwd isn't
// able to decrypt the key it fails. The transaction is returned in RLP-form, not broadcast
// to other nodes
func (s *PersonalAccountAPI) SignTransaction(ctx context.Context, args TransactionArgs, passwd string) (*SignTransactionResult, error) {
	// No need to obtain the noncelock mutex, since we won't be sending this
	// tx into the transaction pool, but right back to the user
	if args.From == nil {
		return nil, errors.New("sender not specified")
	}
	if args.Gas == nil {
		return nil, errors.New("gas not specified")
	}
	if args.GasPrice == nil && (args.MaxFeePerGas == nil || args.MaxPriorityFeePerGas == nil) {
		return nil, errors.New("missing gasPrice or maxFeePerGas/maxPriorityFeePerGas")
	}
	if args.IsEIP4844() {
		return nil, errBlobTxNotSupported
	}
	if args.Nonce == nil {
		return nil, errors.New("nonce not specified")
	}
	// Before actually signing the transaction, ensure the transaction fee is reasonable.
	tx := args.toTransaction()
	if err := checkTxFee(tx.GasPrice(), tx.Gas(), s.b.RPCTxFeeCap()); err != nil {
		return nil, err
	}
	signed, err := s.signTransaction(ctx, &args, passwd)
	if err != nil {
		log.Warn("Failed transaction sign attempt", "from", args.from(), "to", args.To, "value", args.Value.ToInt(), "err", err)
		return nil, err
	}
	data, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &SignTransactionResult{data, signed}, nil
}

// Sign calculates an Ethereum ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message))
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The key used to calculate the signature is decrypted with the given password.
//
// https://geth.ethereum.org/docs/interacting-with-geth/rpc/ns-personal#personal-sign
func (s *PersonalAccountAPI) Sign(ctx context.Context, data hexutil.Bytes, addr common.Address, passwd string) (hexutil.Bytes, error) {
	// Look up the wallet containing the requested signer
	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}
	// Assemble sign the data with the wallet
	signature, err := wallet.SignTextWithPassphrase(account, passwd, data)
	if err != nil {
		log.Warn("Failed data sign attempt", "address", addr, "err", err)
		return nil, err
	}
	signature[crypto.RecoveryIDOffset] += 27 // Transform V from 0/1 to 27/28 according to the yellow paper
	return signature, nil
}

// EcRecover returns the address for the account that was used to create the signature.
// Note, this function is compatible with eth_sign and personal_sign. As such it recovers
// the address of:
// hash = keccak256("\x19Ethereum Signed Message:\n"${message length}${message})
// addr = ecrecover(hash, signature)
//
// Note, the signature must conform to the secp256k1 curve R, S and V values, where
// the V value must be 27 or 28 for legacy reasons.
//
// https://geth.ethereum.org/docs/interacting-with-geth/rpc/ns-personal#personal-ecrecover
func (s *PersonalAccountAPI) EcRecover(ctx context.Context, data, sig hexutil.Bytes) (common.Address, error) {
	if len(sig) != crypto.SignatureLength {
		return common.Address{}, fmt.Errorf("signature must be %d bytes long", crypto.SignatureLength)
	}
	if sig[crypto.RecoveryIDOffset] != 27 && sig[crypto.RecoveryIDOffset] != 28 {
		return common.Address{}, errors.New("invalid Ethereum signature (V is not 27 or 28)")
	}
	sig[crypto.RecoveryIDOffset] -= 27 // Transform yellow paper V from 27/28 to 0/1

	rpk, err := crypto.SigToPub(accounts.TextHash(data), sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*rpk), nil
}

// InitializeWallet initializes a new wallet at the provided URL, by generating and returning a new private key.
func (s *PersonalAccountAPI) InitializeWallet(ctx context.Context, url string) (string, error) {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return "", err
	}

	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return "", err
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", err
	}

	seed := bip39.NewSeed(mnemonic, "")

	switch wallet := wallet.(type) {
	case *scwallet.Wallet:
		return mnemonic, wallet.Initialize(seed)
	default:
		return "", errors.New("specified wallet does not support initialization")
	}
}

// Unpair deletes a pairing between wallet and geth.
func (s *PersonalAccountAPI) Unpair(ctx context.Context, url string, pin string) error {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return err
	}

	switch wallet := wallet.(type) {
	case *scwallet.Wallet:
		return wallet.Unpair([]byte(pin))
	default:
		return errors.New("specified wallet does not support pairing")
	}
}

// BlockChainAPI provides an API to access Ethereum blockchain data.
type BlockChainAPI struct {
	b Backend
}

// NewBlockChainAPI creates a new Ethereum blockchain API.
func NewBlockChainAPI(b Backend) *BlockChainAPI {
	return &BlockChainAPI{b}
}

// ChainId is the EIP-155 replay-protection chain id for the current Ethereum chain config.
//
// Note, this method does not conform to EIP-695 because the configured chain ID is always
// returned, regardless of the current head block. We used to return an error when the chain
// wasn't synced up to a block where EIP-155 is enabled, but this behavior caused issues
// in CL clients.
func (api *BlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(api.b.ChainConfig().ChainID)
}

// BlockNumber returns the block number of the chain head.
func (s *BlockChainAPI) BlockNumber() hexutil.Uint64 {
	header, _ := s.b.HeaderByNumber(context.Background(), rpc.LatestBlockNumber) // latest header should always be available
	return hexutil.Uint64(header.Number.Uint64())
}

// GetBalance returns the amount of wei for the given address in the state of the
// given block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta
// block numbers are also allowed.
func (s *BlockChainAPI) GetBalance(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Big, error) {
	log.Info("通过BlockChainAPI.b.StateAndHeaderByNumberOrHash获取当前链数据库对象")
	state, _, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	b := state.GetBalance(address).ToBig()
	log.Info("通过当前链数据库对象获取余额", "余额", b)
	return (*hexutil.Big)(b), state.Error()
}

// AccountResult structs for GetProof
type AccountResult struct {
	Address      common.Address  `json:"address"`
	AccountProof []string        `json:"accountProof"`
	Balance      *hexutil.Big    `json:"balance"`
	CodeHash     common.Hash     `json:"codeHash"`
	Nonce        hexutil.Uint64  `json:"nonce"`
	StorageHash  common.Hash     `json:"storageHash"`
	StorageProof []StorageResult `json:"storageProof"`
}

type StorageResult struct {
	Key   string       `json:"key"`
	Value *hexutil.Big `json:"value"`
	Proof []string     `json:"proof"`
}

// proofList implements ethdb.KeyValueWriter and collects the proofs as
// hex-strings for delivery to rpc-caller.
type proofList []string

func (n *proofList) Put(key []byte, value []byte) error {
	*n = append(*n, hexutil.Encode(value))
	return nil
}

func (n *proofList) Delete(key []byte) error {
	panic("not supported")
}

// GetProof returns the Merkle-proof for a given account and optionally some storage keys.
func (s *BlockChainAPI) GetProof(ctx context.Context, address common.Address, storageKeys []string, blockNrOrHash rpc.BlockNumberOrHash) (*AccountResult, error) {
	var (
		keys         = make([]common.Hash, len(storageKeys))
		keyLengths   = make([]int, len(storageKeys))
		storageProof = make([]StorageResult, len(storageKeys))
	)
	// Deserialize all keys. This prevents state access on invalid input.
	for i, hexKey := range storageKeys {
		var err error
		keys[i], keyLengths[i], err = decodeHash(hexKey)
		if err != nil {
			return nil, err
		}
	}
	statedb, header, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if statedb == nil || err != nil {
		return nil, err
	}
	codeHash := statedb.GetCodeHash(address)
	storageRoot := statedb.GetStorageRoot(address)

	if len(keys) > 0 {
		var storageTrie state.Trie
		if storageRoot != types.EmptyRootHash && storageRoot != (common.Hash{}) {
			id := trie.StorageTrieID(header.Root, crypto.Keccak256Hash(address.Bytes()), storageRoot)
			st, err := trie.NewStateTrie(id, statedb.Database().TrieDB())
			if err != nil {
				return nil, err
			}
			storageTrie = st
		}
		// Create the proofs for the storageKeys.
		for i, key := range keys {
			// Output key encoding is a bit special: if the input was a 32-byte hash, it is
			// returned as such. Otherwise, we apply the QUANTITY encoding mandated by the
			// JSON-RPC spec for getProof. This behavior exists to preserve backwards
			// compatibility with older client versions.
			var outputKey string
			if keyLengths[i] != 32 {
				outputKey = hexutil.EncodeBig(key.Big())
			} else {
				outputKey = hexutil.Encode(key[:])
			}
			if storageTrie == nil {
				storageProof[i] = StorageResult{outputKey, &hexutil.Big{}, []string{}}
				continue
			}
			var proof proofList
			if err := storageTrie.Prove(crypto.Keccak256(key.Bytes()), &proof); err != nil {
				return nil, err
			}
			value := (*hexutil.Big)(statedb.GetState(address, key).Big())
			storageProof[i] = StorageResult{outputKey, value, proof}
		}
	}
	// Create the accountProof.
	tr, err := trie.NewStateTrie(trie.StateTrieID(header.Root), statedb.Database().TrieDB())
	if err != nil {
		return nil, err
	}
	var accountProof proofList
	if err := tr.Prove(crypto.Keccak256(address.Bytes()), &accountProof); err != nil {
		return nil, err
	}
	balance := statedb.GetBalance(address).ToBig()
	return &AccountResult{
		Address:      address,
		AccountProof: accountProof,
		Balance:      (*hexutil.Big)(balance),
		CodeHash:     codeHash,
		Nonce:        hexutil.Uint64(statedb.GetNonce(address)),
		StorageHash:  storageRoot,
		StorageProof: storageProof,
	}, statedb.Error()
}

// decodeHash parses a hex-encoded 32-byte hash. The input may optionally
// be prefixed by 0x and can have a byte length up to 32.
func decodeHash(s string) (h common.Hash, inputLength int, err error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	if (len(s) & 1) > 0 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, 0, errors.New("hex string invalid")
	}
	if len(b) > 32 {
		return common.Hash{}, len(b), errors.New("hex string too long, want at most 32 bytes")
	}
	return common.BytesToHash(b), len(b), nil
}

// GetHeaderByNumber returns the requested canonical block header.
//   - When blockNr is -1 the chain pending header is returned.
//   - When blockNr is -2 the chain latest header is returned.
//   - When blockNr is -3 the chain finalized header is returned.
//   - When blockNr is -4 the chain safe header is returned.
func (s *BlockChainAPI) GetHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (map[string]interface{}, error) {
	header, err := s.b.HeaderByNumber(ctx, number)
	if header != nil && err == nil {
		response := s.rpcMarshalHeader(ctx, header)
		if number == rpc.PendingBlockNumber {
			// Pending header need to nil out a few fields
			for _, field := range []string{"hash", "nonce", "miner"} {
				response[field] = nil
			}
		}
		return response, err
	}
	return nil, err
}

// GetHeaderByHash returns the requested header by hash.
func (s *BlockChainAPI) GetHeaderByHash(ctx context.Context, hash common.Hash) map[string]interface{} {
	header, _ := s.b.HeaderByHash(ctx, hash)
	if header != nil {
		return s.rpcMarshalHeader(ctx, header)
	}
	return nil
}

// GetBlockByNumber returns the requested canonical block.
//   - When blockNr is -1 the chain pending block is returned.
//   - When blockNr is -2 the chain latest block is returned.
//   - When blockNr is -3 the chain finalized block is returned.
//   - When blockNr is -4 the chain safe block is returned.
//   - When fullTx is true all transactions in the block are returned, otherwise
//     only the transaction hash is returned.
func (s *BlockChainAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	log.Info("开始执行BlockChainAPI.GetBlockByNumber方法", "，BlockNumber", number)
	block, err := s.b.BlockByNumber(ctx, number)
	if block != nil && err == nil {
		response, err := s.rpcMarshalBlock(ctx, block, true, fullTx)
		if err == nil && number == rpc.PendingBlockNumber {
			// Pending blocks need to nil out a few fields
			for _, field := range []string{"hash", "nonce", "miner"} {
				response[field] = nil
			}
		}
		return response, err
	}
	return nil, err
}

// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (s *BlockChainAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]interface{}, error) {
	block, err := s.b.BlockByHash(ctx, hash)
	if block != nil {
		return s.rpcMarshalBlock(ctx, block, true, fullTx)
	}
	return nil, err
}

func (s *BlockChainAPI) Health() bool {
	if rpc.RpcServingTimer != nil {
		return rpc.RpcServingTimer.Snapshot().Percentile(0.75) < float64(UnHealthyTimeout)
	}
	return true
}

// GetFinalizedHeader returns the requested finalized block header.
//   - probabilisticFinalized should be in range [2,21],
//     then the block header with number `max(fastFinalized, latest-probabilisticFinalized)` is returned
func (s *BlockChainAPI) GetFinalizedHeader(ctx context.Context, probabilisticFinalized int64) (map[string]interface{}, error) {
	if probabilisticFinalized < 2 || probabilisticFinalized > 21 {
		return nil, fmt.Errorf("%d out of range [2,21]", probabilisticFinalized)
	}

	var err error
	fastFinalizedHeader, err := s.b.HeaderByNumber(ctx, rpc.FinalizedBlockNumber)
	if err != nil { // impossible
		return nil, err
	}
	latestHeader, err := s.b.HeaderByNumber(ctx, rpc.LatestBlockNumber)
	if err != nil { // impossible
		return nil, err
	}
	finalizedBlockNumber := max(fastFinalizedHeader.Number.Int64(), latestHeader.Number.Int64()-probabilisticFinalized)

	return s.GetHeaderByNumber(ctx, rpc.BlockNumber(finalizedBlockNumber))
}

// GetFinalizedBlock returns the requested finalized block.
//   - probabilisticFinalized should be in range [2,21],
//     then the block with number `max(fastFinalized, latest-probabilisticFinalized)` is returned
//   - When fullTx is true all transactions in the block are returned, otherwise
//     only the transaction hash is returned.
func (s *BlockChainAPI) GetFinalizedBlock(ctx context.Context, probabilisticFinalized int64, fullTx bool) (map[string]interface{}, error) {
	if probabilisticFinalized < 2 || probabilisticFinalized > 21 {
		return nil, fmt.Errorf("%d out of range [2,21]", probabilisticFinalized)
	}

	var err error
	fastFinalizedHeader, err := s.b.HeaderByNumber(ctx, rpc.FinalizedBlockNumber)
	if err != nil { // impossible
		return nil, err
	}
	latestHeader, err := s.b.HeaderByNumber(ctx, rpc.LatestBlockNumber)
	if err != nil { // impossible
		return nil, err
	}
	finalizedBlockNumber := max(fastFinalizedHeader.Number.Int64(), latestHeader.Number.Int64()-probabilisticFinalized)

	return s.GetBlockByNumber(ctx, rpc.BlockNumber(finalizedBlockNumber), fullTx)
}

// GetUncleByBlockNumberAndIndex returns the uncle block for the given block hash and index.
func (s *BlockChainAPI) GetUncleByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) (map[string]interface{}, error) {
	block, err := s.b.BlockByNumber(ctx, blockNr)
	if block != nil {
		uncles := block.Uncles()
		if index >= hexutil.Uint(len(uncles)) {
			log.Debug("Requested uncle not found", "number", blockNr, "hash", block.Hash(), "index", index)
			return nil, nil
		}
		block = types.NewBlockWithHeader(uncles[index])
		return s.rpcMarshalBlock(ctx, block, false, false)
	}
	return nil, err
}

// GetUncleByBlockHashAndIndex returns the uncle block for the given block hash and index.
func (s *BlockChainAPI) GetUncleByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) (map[string]interface{}, error) {
	block, err := s.b.BlockByHash(ctx, blockHash)
	if block != nil {
		uncles := block.Uncles()
		if index >= hexutil.Uint(len(uncles)) {
			log.Debug("Requested uncle not found", "number", block.Number(), "hash", blockHash, "index", index)
			return nil, nil
		}
		block = types.NewBlockWithHeader(uncles[index])
		return s.rpcMarshalBlock(ctx, block, false, false)
	}
	return nil, err
}

// GetUncleCountByBlockNumber returns number of uncles in the block for the given block number
func (s *BlockChainAPI) GetUncleCountByBlockNumber(ctx context.Context, blockNr rpc.BlockNumber) *hexutil.Uint {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		n := hexutil.Uint(len(block.Uncles()))
		return &n
	}
	return nil
}

// GetUncleCountByBlockHash returns number of uncles in the block for the given block hash
func (s *BlockChainAPI) GetUncleCountByBlockHash(ctx context.Context, blockHash common.Hash) *hexutil.Uint {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		n := hexutil.Uint(len(block.Uncles()))
		return &n
	}
	return nil
}

// GetCode returns the code stored at the given address in the state for the given block number.
func (s *BlockChainAPI) GetCode(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	state, _, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	code := state.GetCode(address)
	return code, state.Error()
}

// GetStorageAt returns the storage from the state at the given address, key and
// block number. The rpc.LatestBlockNumber and rpc.PendingBlockNumber meta block
// numbers are also allowed.
func (s *BlockChainAPI) GetStorageAt(ctx context.Context, address common.Address, hexKey string, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	state, _, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	key, _, err := decodeHash(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %s", err)
	}
	res := state.GetState(address, key)
	return res[:], state.Error()
}

// GetBlockReceipts returns the block receipts for the given block hash or number or tag.
func (s *BlockChainAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]map[string]interface{}, error) {
	block, err := s.b.BlockByNumberOrHash(ctx, blockNrOrHash)
	if block == nil || err != nil {
		// When the block doesn't exist, the RPC method should return JSON null
		// as per specification.
		return nil, nil
	}
	receipts, err := s.b.GetReceipts(ctx, block.Hash())
	if err != nil {
		return nil, err
	}
	txs := block.Transactions()
	if len(txs) != len(receipts) {
		return nil, fmt.Errorf("receipts length mismatch: %d vs %d", len(txs), len(receipts))
	}

	// Derive the sender.
	signer := types.MakeSigner(s.b.ChainConfig(), block.Number(), block.Time())

	result := make([]map[string]interface{}, len(receipts))
	for i, receipt := range receipts {
		result[i] = marshalReceipt(receipt, block.Hash(), block.NumberU64(), signer, txs[i], i)
	}

	return result, nil
}

func (s *BlockChainAPI) GetBlobSidecars(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash, fullBlob *bool) ([]map[string]interface{}, error) {
	showBlob := true
	if fullBlob != nil {
		showBlob = *fullBlob
	}
	header, err := s.b.HeaderByNumberOrHash(ctx, blockNrOrHash)
	if header == nil || err != nil {
		// When the block doesn't exist, the RPC method should return JSON null
		// as per specification.
		return nil, nil
	}
	blobSidecars, err := s.b.GetBlobSidecars(ctx, header.Hash())
	if err != nil || blobSidecars == nil {
		return nil, nil
	}
	result := make([]map[string]interface{}, len(blobSidecars))
	for i, sidecar := range blobSidecars {
		result[i] = marshalBlobSidecar(sidecar, showBlob)
	}
	return result, nil
}

func (s *BlockChainAPI) GetBlobSidecarByTxHash(ctx context.Context, hash common.Hash, fullBlob *bool) (map[string]interface{}, error) {
	showBlob := true
	if fullBlob != nil {
		showBlob = *fullBlob
	}
	txTarget, blockHash, _, Index := rawdb.ReadTransaction(s.b.ChainDb(), hash)
	if txTarget == nil {
		return nil, nil
	}
	block, err := s.b.BlockByHash(ctx, blockHash)
	if block == nil || err != nil {
		// When the block doesn't exist, the RPC method should return JSON null
		// as per specification.
		return nil, nil
	}
	blobSidecars, err := s.b.GetBlobSidecars(ctx, blockHash)
	if err != nil || blobSidecars == nil || len(blobSidecars) == 0 {
		return nil, nil
	}
	for _, sidecar := range blobSidecars {
		if sidecar.TxIndex == Index {
			return marshalBlobSidecar(sidecar, showBlob), nil
		}
	}

	return nil, nil
}

// OverrideAccount indicates the overriding fields of account during the execution
// of a message call.
// Note, state and stateDiff can't be specified at the same time. If state is
// set, message execution will only use the data in the given state. Otherwise
// if statDiff is set, all diff will be applied first and then execute the call
// message.
type OverrideAccount struct {
	Nonce     *hexutil.Uint64              `json:"nonce"`
	Code      *hexutil.Bytes               `json:"code"`
	Balance   **hexutil.Big                `json:"balance"`
	State     *map[common.Hash]common.Hash `json:"state"`
	StateDiff *map[common.Hash]common.Hash `json:"stateDiff"`
}

// StateOverride is the collection of overridden accounts.
type StateOverride map[common.Address]OverrideAccount

// Apply overrides the fields of specified accounts into the given state.
func (diff *StateOverride) Apply(state *state.StateDB) error {
	if diff == nil {
		return nil
	}
	for addr, account := range *diff {
		// Override account nonce.
		if account.Nonce != nil {
			state.SetNonce(addr, uint64(*account.Nonce))
		}
		// Override account(contract) code.
		if account.Code != nil {
			state.SetCode(addr, *account.Code)
		}
		// Override account balance.
		if account.Balance != nil {
			u256Balance, _ := uint256.FromBig((*big.Int)(*account.Balance))
			state.SetBalance(addr, u256Balance)
		}
		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}
		// Replace entire state if caller requires.
		if account.State != nil {
			state.SetStorage(addr, *account.State)
		}
		// Apply state diff into specified accounts.
		if account.StateDiff != nil {
			for key, value := range *account.StateDiff {
				state.SetState(addr, key, value)
			}
		}
	}
	// Now finalize the changes. Finalize is normally performed between transactions.
	// By using finalize, the overrides are semantically behaving as
	// if they were created in a transaction just before the tracing occur.
	state.Finalise(false)
	return nil
}

// BlockOverrides is a set of header fields to override.
type BlockOverrides struct {
	Number      *hexutil.Big
	Difficulty  *hexutil.Big
	Time        *hexutil.Uint64
	GasLimit    *hexutil.Uint64
	Coinbase    *common.Address
	Random      *common.Hash
	BaseFee     *hexutil.Big
	BlobBaseFee *hexutil.Big
}

// Apply overrides the given header fields into the given block context.
func (diff *BlockOverrides) Apply(blockCtx *vm.BlockContext) {
	if diff == nil {
		return
	}
	if diff.Number != nil {
		blockCtx.BlockNumber = diff.Number.ToInt()
	}
	if diff.Difficulty != nil {
		blockCtx.Difficulty = diff.Difficulty.ToInt()
	}
	if diff.Time != nil {
		blockCtx.Time = uint64(*diff.Time)
	}
	if diff.GasLimit != nil {
		blockCtx.GasLimit = uint64(*diff.GasLimit)
	}
	if diff.Coinbase != nil {
		blockCtx.Coinbase = *diff.Coinbase
	}
	if diff.Random != nil {
		blockCtx.Random = diff.Random
	}
	if diff.BaseFee != nil {
		blockCtx.BaseFee = diff.BaseFee.ToInt()
	}
	if diff.BlobBaseFee != nil {
		blockCtx.BlobBaseFee = diff.BlobBaseFee.ToInt()
	}
}

// ChainContextBackend provides methods required to implement ChainContext.
type ChainContextBackend interface {
	Engine() consensus.Engine
	HeaderByNumber(context.Context, rpc.BlockNumber) (*types.Header, error)
}

// ChainContext is an implementation of core.ChainContext. It's main use-case
// is instantiating a vm.BlockContext without having access to the BlockChain object.
type ChainContext struct {
	b   ChainContextBackend
	ctx context.Context
}

// NewChainContext creates a new ChainContext object.
func NewChainContext(ctx context.Context, backend ChainContextBackend) *ChainContext {
	return &ChainContext{ctx: ctx, b: backend}
}

func (context *ChainContext) Engine() consensus.Engine {
	return context.b.Engine()
}

func (context *ChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	// This method is called to get the hash for a block number when executing the BLOCKHASH
	// opcode. Hence no need to search for non-canonical blocks.
	header, err := context.b.HeaderByNumber(context.ctx, rpc.BlockNumber(number))
	if err != nil || header.Hash() != hash {
		return nil
	}
	return header
}

func doCall(ctx context.Context, b Backend, args TransactionArgs, state *state.StateDB, header *types.Header, overrides *StateOverride, blockOverrides *BlockOverrides, timeout time.Duration, globalGasCap uint64) (*core.ExecutionResult, error) {
	if err := overrides.Apply(state); err != nil {
		return nil, err
	}
	// Setup context so it may be cancelled the call has completed
	// or, in case of unmetered gas, setup a context with a timeout.
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	// Get a new instance of the EVM.
	blockCtx := core.NewEVMBlockContext(header, NewChainContext(ctx, b), nil)
	if blockOverrides != nil {
		blockOverrides.Apply(&blockCtx)
	}
	msg, err := args.ToMessage(globalGasCap, blockCtx.BaseFee)
	if err != nil {
		return nil, err
	}
	evm := b.GetEVM(ctx, msg, state, header, &vm.Config{NoBaseFee: true}, &blockCtx)

	// Wait for the context to be done and cancel the evm. Even if the
	// EVM has finished, cancelling may be done (repeatedly)
	gopool.Submit(func() {
		<-ctx.Done()
		evm.Cancel()
	})

	// Execute the message.
	gp := new(core.GasPool).AddGas(math.MaxUint64)
	result, err := core.ApplyMessage(evm, msg, gp)
	if err := state.Error(); err != nil {
		return nil, err
	}

	// If the timer caused an abort, return an appropriate error message
	if evm.Cancelled() {
		return nil, fmt.Errorf("execution aborted (timeout = %v)", timeout)
	}
	if err != nil {
		return result, fmt.Errorf("err: %w (supplied gas %d)", err, msg.GasLimit)
	}
	return result, nil
}

func DoCall(ctx context.Context, b Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride, blockOverrides *BlockOverrides, timeout time.Duration, globalGasCap uint64) (*core.ExecutionResult, error) {
	defer func(start time.Time) { log.Debug("Executing EVM call finished", "runtime", time.Since(start)) }(time.Now())

	state, header, err := b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}

	return doCall(ctx, b, args, state, header, overrides, blockOverrides, timeout, globalGasCap)
}

func FlagDoCall(ctx context.Context, b Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride, blockOverrides *BlockOverrides, timeout time.Duration, globalGasCap uint64) (*core.ExecutionResult, error) {
	defer func(start time.Time) { log.Debug("Executing EVM call finished", "runtime", time.Since(start)) }(time.Now())

	state, header, err := b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	state.Flag = 1

	return doCall(ctx, b, args, state, header, overrides, blockOverrides, timeout, globalGasCap)
}

// Call executes the given transaction on the state for the given block number.
//
// Additionally, the caller can specify a batch of contract for fields overriding.
//
// Note, this function doesn't make and changes in the state/blockchain and is
// useful to execute and retrieve values.
func (s *BlockChainAPI) Call(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *StateOverride, blockOverrides *BlockOverrides) (hexutil.Bytes, error) {
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	result, err := DoCall(ctx, s.b, args, *blockNrOrHash, overrides, blockOverrides, s.b.RPCEVMTimeout(), s.b.RPCGasCap())
	if err != nil {
		return nil, err
	}
	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result.Revert())
	}
	return result.Return(), result.Err
}

func (s *BlockChainAPI) FlagCall(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *StateOverride, blockOverrides *BlockOverrides) (hexutil.Bytes, error) {
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	result, err := FlagDoCall(ctx, s.b, args, *blockNrOrHash, overrides, blockOverrides, s.b.RPCEVMTimeout(), s.b.RPCGasCap())
	if err != nil {
		return nil, err
	}
	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result.Revert())
	}
	return result.Return(), result.Err
}

// func worker(s *BlockChainAPI, results chan<- interface{}, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) {
// 	// 设置上下文，用于控制每个任务方法执行超时时间
// 	ctx := context.Background()
// 	call, err := s.FlagCall(ctx, args, blockNrOrHash, nil, nil)
// 	if err != nil {
// 		results <- err
// 	} else {
// 		results <- call
// 	}
// }

func workerDirect(s *BlockChainAPI, results chan<- interface{}, triangle pairtypes.Triangle) {
	// 设置上下文，用于控制每个任务方法执行超时时间
	ctx := context.Background()
	triangular := &pairtypes.ITriangularArbitrageTriangular{
		Token0:  common.HexToAddress(triangle.Token0),
		Router0: common.HexToAddress(triangle.Router0),
		Pair0:   common.HexToAddress(triangle.Pair0),
		Token1:  common.HexToAddress(triangle.Token1),
		Router1: common.HexToAddress(triangle.Router1),
		Pair1:   common.HexToAddress(triangle.Pair1),
		Token2:  common.HexToAddress(triangle.Token2),
		Router2: common.HexToAddress(triangle.Router2),
		Pair2:   common.HexToAddress(triangle.Pair2),
	}

	param := getArbitrageQueryParam(big.NewInt(0), 0, 10000)
	index, err := directResolveIndex(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	log.Info("10000step", "start", param.Start, "end", param.End, "step", param.Pieces, "index", index)

	param = getArbitrageQueryParam(param.Start, index, 1000)
	index, err = directResolveIndex(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	log.Info("1000step", "start", param.Start, "end", param.End, "step", param.Pieces, "index", index)

	param = getArbitrageQueryParam(param.Start, index, 100)
	index, err = directResolveIndex(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	log.Info("100step", "start", param.Start, "end", param.End, "step", param.Pieces, "index", index)

	param = getArbitrageQueryParam(param.Start, index, 10)
	index, err = directResolveIndex(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	log.Info("10step", "start", param.Start, "end", param.End, "step", param.Pieces, "index", index)

	point := new(big.Int).Add(param.Start, big.NewInt(int64(index)))
	if point.Cmp(big.NewInt(0)) == 0 {
		results <- nil
		return
	}
	param.Start = point
	param.End = point
	param.Pieces = big.NewInt(1)

	call, err := getRoisDirect(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	roisBytes := call[32*2:]
	roisStr := hex.EncodeToString(roisBytes)
	var rois []string
	for i := 0; i < len(roisStr)/64; i++ {
		rois[i] = roisStr[i*64 : (i+1)*64]
	}

	roi13 := new(big.Int).SetBytes(roisBytes[32*12 : 32*13])
	if call == nil || roi13.Cmp(big.NewInt(5000000)) < 0 {
		results <- nil
		return
	}

	snapshotsHash := solsha3.SoliditySHA3(rois[3], rois[4], rois[5])
	subHex := hex.EncodeToString(snapshotsHash)[0:2]

	parameters := []interface{}{
		hex.EncodeToString(solsha3.Uint32(big.NewInt(0))),
		subHex,
		rois[0][24:],
		rois[6][40:],
		rois[1][24:],
		rois[7][40:],
		rois[2][24],
		rois[10][40:],
		triangular.Token0,
		rois[11][40:],
		triangular.Pair0,
		rois[12][40:],
		triangular.Token1,
		rois[13][40:],
		triangular.Pair1,
		triangular.Token2,
		triangular.Pair2,
	}

	calldata, err := EncodePackedBsc(parameters)
	if err != nil {
		results <- err
		return
	}

	ROI := &ROI{
		Triangle: triangle,
		CallData: calldata,
		Profit:   *roi13,
	}

	results <- ROI
	return
}

func workerTest(s *BlockChainAPI, results chan<- interface{}, triangle pairtypes.Triangle) {
	// 设置上下文，用于控制每个任务方法执行超时时间
	ctx := context.Background()
	triangular := &pairtypes.ITriangularArbitrageTriangular{
		Token0:  common.HexToAddress(triangle.Token0),
		Router0: common.HexToAddress(triangle.Router0),
		Pair0:   common.HexToAddress(triangle.Pair0),
		Token1:  common.HexToAddress(triangle.Token1),
		Router1: common.HexToAddress(triangle.Router1),
		Pair1:   common.HexToAddress(triangle.Pair1),
		Token2:  common.HexToAddress(triangle.Token2),
		Router2: common.HexToAddress(triangle.Router2),
		Pair2:   common.HexToAddress(triangle.Pair2),
	}

	param := getArbitrageQueryParam(big.NewInt(0), 0, 10000)
	rois, err := getRoisTest(s, triangular, param, ctx)
	log.Info("10000step", "start", param.Start, "end", param.End, "step", param.Pieces, "rois", rois)
	if err != nil {
		results <- err
		return
	}

	index := resolveROI(rois)
	param = getArbitrageQueryParam(param.Start, index, 1000)
	rois, err = getRoisTest(s, triangular, param, ctx)
	log.Info("1000step", "start", param.Start, "end", param.End, "step", param.Pieces, "rois", rois)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)

	param = getArbitrageQueryParam(param.Start, index, 100)
	rois, err = getRoisTest(s, triangular, param, ctx)
	log.Info("100step", "start", param.Start, "end", param.End, "step", param.Pieces, "rois", rois)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)

	param = getArbitrageQueryParam(param.Start, index, 10)
	rois, err = getRoisTest(s, triangular, param, ctx)
	log.Info("10step", "start", param.Start, "end", param.End, "step", param.Pieces, "rois", rois)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)
	point := new(big.Int).Add(param.Start, big.NewInt(int64(index)))
	if point.Cmp(big.NewInt(0)) == 0 {
		results <- nil
		return
	}
	param.Start = point
	param.End = point
	param.Pieces = big.NewInt(1)

	rois, err = getRoisTest(s, triangular, param, ctx)
	log.Info("point", "start", param.Start, "end", param.End, "step", param.Pieces, "rois", rois)
	if err != nil {
		results <- err
		return
	}

	if rois == nil || rois[13] == nil || rois[13].Cmp(big.NewInt(5000000)) < 0 {
		results <- nil
		return
	}

	snapshotsHash := solsha3.SoliditySHA3(solsha3.Int256(rois[3]), solsha3.Int256(rois[4]), solsha3.Int256(rois[5]))
	subHex := hex.EncodeToString(snapshotsHash)[0:2]

	parameters := []interface{}{
		hex.EncodeToString(solsha3.Uint32(big.NewInt(0))),
		subHex,
		common.BigToAddress(rois[0]),
		getWei(rois[6], 96),
		common.BigToAddress(rois[1]),
		getWei(rois[7], 96),
		common.BigToAddress(rois[2]),
		getWei(rois[10], 96),
		triangular.Token0,
		getWei(rois[11], 96),
		triangular.Pair0,
		getWei(rois[12], 96),
		triangular.Token1,
		getWei(rois[13], 96),
		triangular.Pair1,
		triangular.Token2,
		triangular.Pair2,
	}

	calldata, err := EncodePackedBsc(parameters)
	if err != nil {
		results <- err
		return
	}

	ROI := &ROI{
		Triangle: triangle,
		CallData: calldata,
		Profit:   *rois[13],
	}

	results <- ROI
	return
}

func pairWorker(s *BlockChainAPI, results chan<- interface{}, triangle pairtypes.Triangle) {
	// 设置上下文，用于控制每个任务方法执行超时时间
	ctx := context.Background()
	triangular := &pairtypes.ITriangularArbitrageTriangular{
		Token0:  common.HexToAddress(triangle.Token0),
		Router0: common.HexToAddress(triangle.Router0),
		Pair0:   common.HexToAddress(triangle.Pair0),
		Token1:  common.HexToAddress(triangle.Token1),
		Router1: common.HexToAddress(triangle.Router1),
		Pair1:   common.HexToAddress(triangle.Pair1),
		Token2:  common.HexToAddress(triangle.Token2),
		Router2: common.HexToAddress(triangle.Router2),
		Pair2:   common.HexToAddress(triangle.Pair2),
	}

	param := getArbitrageQueryParam(big.NewInt(0), 0, 10000)
	rois, err := getRois(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}

	index := resolveROI(rois)
	param = getArbitrageQueryParam(param.Start, index, 1000)
	rois, err = getRois(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)

	param = getArbitrageQueryParam(param.Start, index, 100)
	rois, err = getRois(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)

	param = getArbitrageQueryParam(param.Start, index, 10)
	rois, err = getRois(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}
	index = resolveROI(rois)
	point := new(big.Int).Add(param.Start, big.NewInt(int64(index)))
	if point.Cmp(big.NewInt(0)) == 0 {
		results <- nil
		return
	}
	param.Start = point
	param.End = point
	param.Pieces = big.NewInt(1)

	rois, err = getRois(s, triangular, param, ctx)
	if err != nil {
		results <- err
		return
	}

	if rois == nil || rois[13] == nil || rois[13].Cmp(big.NewInt(5000000)) < 0 {
		results <- nil
		return
	}

	snapshotsHash := solsha3.SoliditySHA3(solsha3.Int256(rois[3]), solsha3.Int256(rois[4]), solsha3.Int256(rois[5]))
	subHex := hex.EncodeToString(snapshotsHash)[0:2]

	parameters := []interface{}{
		hex.EncodeToString(solsha3.Uint32(big.NewInt(0))),
		subHex,
		common.BigToAddress(rois[0]),
		getWei(rois[6], 96),
		common.BigToAddress(rois[1]),
		getWei(rois[7], 96),
		common.BigToAddress(rois[2]),
		getWei(rois[10], 96),
		triangular.Token0,
		getWei(rois[11], 96),
		triangular.Pair0,
		getWei(rois[12], 96),
		triangular.Token1,
		getWei(rois[13], 96),
		triangular.Pair1,
		triangular.Token2,
		triangular.Pair2,
	}

	calldata, err := EncodePackedBsc(parameters)
	if err != nil {
		results <- err
		return
	}

	ROI := &ROI{
		Triangle: triangle,
		CallData: calldata,
		Profit:   *rois[13],
	}

	results <- ROI
	return
}

func EncodePackedBsc(values []interface{}) (string, error) {
	var encoded string
	for _, value := range values {
		switch v := value.(type) {
		case string:
			encoded = encoded + v
		case *Wei:
			wei := *v
			encoded = encoded + wei.Data[len(wei.Data)-wei.BitSize/4:]
		case common.Address:
			addrStr := v.Hex()[2:]
			encoded = encoded + addrStr
		default:
			return "", fmt.Errorf("unsupported type: %T", value)
		}
	}
	return encoded, nil
}

type Wei struct {
	BitSize int
	Data    string
}

func getWei(roi *big.Int, bitSize int) *Wei {
	return &Wei{
		BitSize: bitSize,
		Data:    hex.EncodeToString(solsha3.Int256(roi)),
	}
}

type ROI struct {
	Triangle pairtypes.Triangle
	CallData string
	Profit   big.Int
}

func getRois(s *BlockChainAPI, triangular *pairtypes.ITriangularArbitrageTriangular, param *ArbitrageQueryParam, ctx context.Context) ([]*big.Int, error) {
	data, _ := pair.Encoder("arbitrageQuery", triangular, param.Start, param.End, param.Pieces)
	bytes := hexutil.Bytes(data)
	args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
	call, err := s.FlagCall(ctx, args, &pair.LatestBlockNumber, nil, nil)
	if err != nil {
		return nil, err
	} else {
		roiStr := hex.EncodeToString(call)
		lenth := len(roiStr) / 64
		rois := make([]*big.Int, lenth-2)
		for j := 0; j < lenth; j++ {
			if j > 1 {
				roi, _ := new(big.Int).SetString(roiStr[64*j:64*(j+1)], 16)
				rois[j-2] = roi
			}
		}
		return rois, err
	}
}

func getRoisTest(s *BlockChainAPI, triangular *pairtypes.ITriangularArbitrageTriangular, param *ArbitrageQueryParam, ctx context.Context) ([]*big.Int, error) {
	data, _ := pair.Encoder("arbitrageQuery", triangular, param.Start, param.End, param.Pieces)
	bytes := hexutil.Bytes(data)
	args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
	call, err := s.FlagCall(ctx, args, &pair.LatestBlockNumber, nil, nil)
	if err != nil {
		return nil, err
	} else {
		roiStr := hex.EncodeToString(call)
		lenth := len(roiStr) / 64
		rois := make([]*big.Int, lenth-2)
		for j := 0; j < lenth; j++ {
			subStr := roiStr[64*j : 64*(j+1)]
			log.Info("CallReturn EncodeToString", "roiStr", subStr)
			if j > 1 {
				roi, _ := new(big.Int).SetString(subStr, 16)
				rois[j-2] = roi
			}
		}
		return rois, err
	}
}

func getRoisDirect(s *BlockChainAPI, triangular *pairtypes.ITriangularArbitrageTriangular, param *ArbitrageQueryParam, ctx context.Context) (hexutil.Bytes, error) {
	data, _ := pair.Encoder("arbitrageQuery", triangular, param.Start, param.End, param.Pieces)
	bytes := hexutil.Bytes(data)
	args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
	return s.FlagCall(ctx, args, &pair.LatestBlockNumber, nil, nil)
}

type ArbitrageQueryParam struct {
	Start  *big.Int
	End    *big.Int
	Pieces *big.Int
}

func getArbitrageQueryParam(start *big.Int, index, step int) *ArbitrageQueryParam {
	if index >= 10 {
		index = 9
	}
	// 计算 startNew = start + step * index
	stepBigInt := big.NewInt(int64(step))
	indexBigInt := big.NewInt(int64(index))
	startNew := new(big.Int).Add(start, new(big.Int).Mul(stepBigInt, indexBigInt))

	// 计算 end = startNew + step
	end := new(big.Int).Add(startNew, stepBigInt)

	// 返回查询参数
	return &ArbitrageQueryParam{
		Start:  startNew,
		End:    end,
		Pieces: big.NewInt(10), // 相当于 BigInteger.TEN
	}
}

func resolveROI(rois []*big.Int) int {
	var i int
	// 排除rois前6个元素，剩下元素每8个一组，循环每组中首元素判断是否为0
	for i = 0; i < (len(rois)-6)/8; i++ {
		if rois[i*8+6].Cmp(big.NewInt(0)) == 0 {
			return i
		}
	}
	return i
}

func directResolveIndex(s *BlockChainAPI, triangular *pairtypes.ITriangularArbitrageTriangular, param *ArbitrageQueryParam, ctx context.Context) (int, error) {
	data, _ := pair.Encoder("arbitrageQuery", triangular, param.Start, param.End, param.Pieces)
	bytes := hexutil.Bytes(data)
	args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
	call, err := s.FlagCall(ctx, args, &pair.LatestBlockNumber, nil, nil)
	var i int
	if err != nil {
		return i, err
	}

	// 截取掉前8个长度为32个字节的元素，获取roi利润字节部分数据，同样这些数据每32个字节长度代表一个元素，并将元素每8个分成一组（正常数据能得到10组数据，每组索引0-9）
	roiCall := call[32*8:]
	lenth := len(roiCall) / 32 / 8

	// 从第一组开始循环，将组内首个字节元素转换为big.int类型，判断其值是否等于0，等于0代表无利润了，返回该组的索引
	for i = 0; i < lenth; i++ {
		if new(big.Int).SetBytes(roiCall[i*8*32:i*8*32+32]).Cmp(big.NewInt(0)) == 0 {
			return i, nil
		}
	}
	return i, nil
}

type CallBatchArgs struct {
	Args           TransactionArgs
	BlockNrOrHash  *rpc.BlockNumberOrHash
	Overrides      *StateOverride
	BlockOverrides *BlockOverrides
}

type Results struct {
	GetDatasSince time.Duration          `json:"getDatasSince"`
	SelectSince   time.Duration          `json:"selectSince"`
	TotalSince    time.Duration          `json:"totalSince"`
	ResultMap     map[string]interface{} `json:"resultMap"`
}

func GetEthCallData() ([]CallBatchArgs, error) {
	// 打开测试数据文件
	file, err := os.Open("/bc/bsc/build/bin/testdata.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return nil, err
	}
	defer file.Close()

	// 创建一个缓冲读取器
	scanner := bufio.NewScanner(file)

	datas := make([]CallBatchArgs, 0, 10000)
	for scanner.Scan() {
		line := scanner.Text()
		batchArgs := CallBatchArgs{Overrides: nil, BlockOverrides: nil}
		// 从目标字符串之后开始提取内容
		index1 := strings.Index(line, "\"params\":[")
		if index1 != -1 {
			// 提取目标字符串之后的内容
			param1 := line[index1+len("\"params\":[") : len(line)-12]
			err := json.Unmarshal([]byte(param1), &batchArgs.Args)
			if err != nil {
				return nil, err
			}
		}
		index2 := strings.Index(line, "},\"")
		if index2 != -1 {
			// 提取目标字符串之后的内容
			param2 := line[index2+len("},\"") : len(line)-4]
			var num rpc.BlockNumber
			num.UnmarshalJSON([]byte(param2))
			number := rpc.BlockNumberOrHashWithNumber(num)
			batchArgs.BlockNrOrHash = &number
		}
		datas = append(datas, batchArgs)
	}
	return datas, nil
}

func SubmitTestCall(wg *sync.WaitGroup, s *BlockChainAPI, results chan interface{}, triangle *pairtypes.Triangle) {
	t := *triangle
	gopool.Submit(func() {
		defer wg.Done()
		workerTest(s, results, t)
	})
}

func SubmitCall(wg *sync.WaitGroup, s *BlockChainAPI, results chan interface{}, triangle *pairtypes.Triangle) {
	t := *triangle
	gopool.Submit(func() {
		defer wg.Done()
		pairWorker(s, results, t)
	})
}

// CallBatch batch executes Call
// func (s *BlockChainAPI) CallBatch() (string, error) {
// 	// 读取任务测试数据
// 	log.Info("开始执行CallBatch")
// 	datas, err := GetEthCallData()
// 	if err != nil {
// 		return "", err
// 	}
// 	// getDatasSince := time.Since(start)
// 	// log.Info("获取所有测试数据花费时长", "runtime", getDatasSince)
//
// 	// 根据任务数创建结果读取通道
// 	results := make(chan interface{}, len(datas))
//
// 	// 提交任务到协程池，所有协程完成后关闭结果读取通道
// 	start := time.Now()
// 	var wg sync.WaitGroup
// 	for _, job := range datas {
// 		wg.Add(1)
// 		args := job.Args
// 		gopool.Submit(func() {
// 			defer wg.Done()
// 			worker(s, results, args, &pair.LatestBlockNumber)
// 		})
// 	}
// 	wg.Wait()
// 	close(results)
// 	selectSince := time.Since(start)
// 	log.Info("所有eth_call查询任务执行完成花费时长", "runtime", selectSince, "所在的区块号", s.BlockNumber())
//
// 	// 读取任务结果通道数据进行处理
// 	resultMap := make(map[string]interface{}, len(datas))
// 	i := 1
// 	// 处理结果
// 	for result := range results {
// 		itoa := strconv.Itoa(i)
// 		switch v := result.(type) {
// 		case hexutil.Bytes:
// 			bytes := result.(hexutil.Bytes)
// 			if err != nil {
// 				resultMap[itoa] = err.Error()
// 			} else {
// 				dateStr := hex.EncodeToString(bytes)
// 				resultMap["itoa"] = dateStr
// 				lenth := len(dateStr) / 64
// 				roi := make([]*big.Int, lenth-2)
// 				for j := 0; j < lenth; j++ {
// 					subDataStr := dateStr[64*j : 64*(j+1)-1]
// 					resultMap["itoabytes"+strconv.Itoa(j)] = subDataStr
// 					if j > 1 {
// 						setString, _ := new(big.Int).SetString(subDataStr, 16)
// 						roi[j-2] = setString
// 					}
// 				}
// 				log.Info("解析的roi成功", "roi", roi)
// 			}
// 		case error:
// 			resultMap[itoa] = v.Error()
// 		default:
// 			resultMap[itoa] = v
// 		}
// 		i += 1
// 	}
// 	totalSince := time.Since(start)
// 	r := Results{GetDatasSince: 0, SelectSince: selectSince, TotalSince: totalSince, ResultMap: resultMap}
//
// 	// 创建文件
// 	file, err := os.Create("/bc/bsc/build/bin/results.json")
// 	if err != nil {
// 		return "", err
// 	}
// 	defer file.Close()
//
// 	// 将 map 编码为 JSON
// 	encoder := json.NewEncoder(file)
// 	encoder.SetIndent("", "  ") // 设置缩进格式
// 	if err := encoder.Encode(r); err != nil {
// 		return "", err
// 	}
// 	log.Info("结果输出到文件完成，结束")
// 	return "ok", nil
// }

func (s *BlockChainAPI) CallBatch() (string, error) {
	// 读取任务测试数据
	log.Info("开始执行CallBatch")
	var triangles []*pairtypes.Triangle
	oriTriangular := &pairtypes.Triangle{
		ID:      1,
		Token0:  "0xeBBAefF6217d22E7744394061D874015709b8141",
		Router0: "0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865",
		Pair0:   "0x170a4d2A29b30c6551f6a4C0CB527e7A9Cb7D526",
		Token1:  "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c",
		Router1: "0xdB1d10011AD0Ff90774D0C6Bb92e5C5c8b4461F7",
		Pair1:   "0xCB99FE720124129520f7a09Ca3CBEF78D58Ed934",
		Token2:  "0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56",
		Router2: "0x10ED43C718714eb63d5aA57B78B54704E256024E",
		Pair2:   "0xc1fE0336456a8D4550ab0E1e528a684Bcf7bD3F8",
	}
	triangles = append(triangles, oriTriangular)

	// 初始化构造当前区块公共数据
	start := time.Now()
	results := make(chan interface{}, len(triangles))

	// 提交任务到协程池，所有协程完成后关闭结果读取通道
	var wg sync.WaitGroup
	for _, triangle := range triangles {
		wg.Add(1)
		SubmitTestCall(&wg, s, results, triangle)
	}
	wg.Wait()
	close(results)
	selectSince := time.Since(start)
	log.Info("所有eth_call查询任务执行完成花费时长", "runtime", selectSince, "所在的区块号", s.BlockNumber())

	// 读取任务结果通道数据进行处理
	rois := make([]ROI, 0, 5000)
	resultMap := make(map[string]interface{}, len(triangles))
	i := 1
	// 处理结果
	for result := range results {
		itoa := strconv.Itoa(i)
		switch v := result.(type) {
		case *ROI:
			rois = append(rois, *v)
			resultMap[itoa] = *v
		case error:
			resultMap[itoa] = v.Error()
		default:
			resultMap[itoa] = v
		}
		i += 1
	}

	if len(rois) > 0 {
		// 按 Profit 字段对rois进行降序排序
		log.Info("排序前的rois", "rois", rois)
		sort.Slice(rois, func(i, j int) bool {
			return rois[i].Profit.Cmp(&rois[j].Profit) > 0
		})
		log.Info("降序排序rois成功", "rois", rois)

		// 将排序后的rois去重过滤，保证每个pair只能出现一次，重复时将Profit较小的ROI都删除，只保留Profit最大的ROI
		// 去重，保证 Pair0, Pair1, Pair2 中的值只出现一次
		uniquePairs := make(map[string]bool)
		var filteredROIs []ROI
		for _, roi := range rois {
			if uniquePairs[roi.Triangle.Pair0] || uniquePairs[roi.Triangle.Pair1] || uniquePairs[roi.Triangle.Pair2] {
				// 如果任何一个 pair 已经出现过，跳过该结构体（删除）
				continue
			}

			// 如果不存在，则将该结构体加入结果集，并标记 pairs 为已出现
			filteredROIs = append(filteredROIs, roi)
			uniquePairs[roi.Triangle.Pair0] = true
			uniquePairs[roi.Triangle.Pair1] = true
			uniquePairs[roi.Triangle.Pair2] = true
		}
		log.Info("排序去重获rois成功", "filteredROIs", filteredROIs)

		// 计算预估总gas
		var gasTotal hexutil.Uint64
		for _, filteredROI := range filteredROIs {
			decodeString, _ := hex.DecodeString(filteredROI.CallData)
			bytes := hexutil.Bytes(decodeString)
			args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
			gas, err := s.EstimateGas(context.Background(), args, &pair.LatestBlockNumber, nil)
			if err != nil {
				log.Error("存在roi的预估gas计算异常", "err", err)
			}
			gasTotal = gasTotal + gas
		}
		log.Info("计算预估总gas成功", "gasTotal", gasTotal)
	}

	totalSince := time.Since(start)
	r := Results{GetDatasSince: 0, SelectSince: selectSince, TotalSince: totalSince, ResultMap: resultMap}

	// 创建文件
	file, err := os.Create("/bc/bsc/build/bin/results.json")
	if err != nil {
		return "", err
	}
	defer file.Close()

	// 将 map 编码为 JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // 设置缩进格式
	if err := encoder.Encode(r); err != nil {
		return "", err
	}
	log.Info("结果输出到文件完成，结束")
	return "ok", nil
}

// PairCallBatch executes Call
func (s *BlockChainAPI) PairCallBatch(triangles []pairtypes.Triangle) error {
	// 初始化构造当前区块公共数据
	start := time.Now()
	log.Info("开始执行PairCallBatch")
	results := make(chan interface{}, len(triangles))

	// 提交任务到协程池，所有协程完成后关闭结果读取通道
	var wg sync.WaitGroup
	for _, triangle := range triangles {
		wg.Add(1)
		SubmitCall(&wg, s, results, &triangle)
	}
	wg.Wait()
	close(results)
	selectSince := time.Since(start)
	log.Info("所有eth_call查询任务执行完成花费时长", "runtime", selectSince, "所在的区块号", s.BlockNumber())

	// 读取任务结果通道数据进行处理
	rois := make([]ROI, 0, 5000)
	resultMap := make(map[string]interface{}, len(triangles))
	i := 1
	// 处理结果
	for result := range results {
		itoa := strconv.Itoa(i)
		switch v := result.(type) {
		case *ROI:
			rois = append(rois, *v)
		case error:
			resultMap[itoa] = v.Error()
		default:
			resultMap[itoa] = v
		}
		i += 1
	}

	if len(rois) > 0 {
		// 按 Profit 字段对rois进行降序排序
		log.Info("排序前的rois", "rois", rois)
		sort.Slice(rois, func(i, j int) bool {
			return rois[i].Profit.Cmp(&rois[j].Profit) > 0
		})
		log.Info("降序排序rois成功", "rois", rois)

		// 将排序后的rois去重过滤，保证每个pair只能出现一次，重复时将Profit较小的ROI都删除，只保留Profit最大的ROI
		// 去重，保证 Pair0, Pair1, Pair2 中的值只出现一次
		uniquePairs := make(map[string]bool)
		var filteredROIs []ROI
		for _, roi := range rois {
			if uniquePairs[roi.Triangle.Pair0] || uniquePairs[roi.Triangle.Pair1] || uniquePairs[roi.Triangle.Pair2] {
				// 如果任何一个 pair 已经出现过，跳过该结构体（删除）
				continue
			}

			// 如果不存在，则将该结构体加入结果集，并标记 pairs 为已出现
			filteredROIs = append(filteredROIs, roi)
			uniquePairs[roi.Triangle.Pair0] = true
			uniquePairs[roi.Triangle.Pair1] = true
			uniquePairs[roi.Triangle.Pair2] = true
		}
		log.Info("排序去重获rois成功", "filteredROIs", filteredROIs)

		// 计算预估总gas
		var gasTotal hexutil.Uint64
		for _, filteredROI := range filteredROIs {
			decodeString, _ := hex.DecodeString(filteredROI.CallData)
			bytes := hexutil.Bytes(decodeString)
			args := TransactionArgs{From: &pair.From, To: &pair.To, Data: &bytes}
			gas, err := s.EstimateGas(context.Background(), args, &pair.LatestBlockNumber, nil)
			if err != nil {
				log.Error("存在roi的预估gas计算异常", "err", err)
			}
			gasTotal = gasTotal + gas
		}
		log.Info("计算预估总gas成功", "gasTotal", gasTotal)
	}

	totalSince := time.Since(start)
	log.Info("处理结果完成", "共耗时", totalSince)

	return nil
}

// DoEstimateGas returns the lowest possible gas limit that allows the transaction to run
// successfully at block `blockNrOrHash`. It returns error if the transaction would revert, or if
// there are unexpected failures. The gas limit is capped by both `args.Gas` (if non-nil &
// non-zero) and `gasCap` (if non-zero).
func DoEstimateGas(ctx context.Context, b Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride, gasCap uint64) (hexutil.Uint64, error) {
	// Retrieve the base state and mutate it with any overrides
	state, header, err := b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return 0, err
	}
	if err = overrides.Apply(state); err != nil {
		return 0, err
	}
	// Construct the gas estimator option from the user input
	opts := &gasestimator.Options{
		Config:     b.ChainConfig(),
		Chain:      NewChainContext(ctx, b),
		Header:     header,
		State:      state,
		ErrorRatio: estimateGasErrorRatio,
	}
	// Run the gas estimation andwrap any revertals into a custom return
	call, err := args.ToMessage(gasCap, header.BaseFee)
	if err != nil {
		return 0, err
	}
	estimate, revert, err := gasestimator.Estimate(ctx, call, opts, gasCap)
	if err != nil {
		if len(revert) > 0 {
			return 0, newRevertError(revert)
		}
		return 0, err
	}
	return hexutil.Uint64(estimate), nil
}

// EstimateGas returns the lowest possible gas limit that allows the transaction to run
// successfully at block `blockNrOrHash`, or the latest block if `blockNrOrHash` is unspecified. It
// returns error if the transaction would revert or if there are unexpected failures. The returned
// value is capped by both `args.Gas` (if non-nil & non-zero) and the backend's RPCGasCap
// configuration (if non-zero).
// Note: Required blob gas is not computed in this method.
func (s *BlockChainAPI) EstimateGas(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *StateOverride) (hexutil.Uint64, error) {
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	return DoEstimateGas(ctx, s.b, args, bNrOrHash, overrides, s.b.RPCGasCap())
}

func (s *BlockChainAPI) needToReplay(ctx context.Context, block *types.Block, accounts []common.Address) (bool, error) {
	receipts, err := s.b.GetReceipts(ctx, block.Hash())
	if err != nil || len(receipts) != len(block.Transactions()) {
		return false, fmt.Errorf("receipt incorrect for block number (%d): %v", block.NumberU64(), err)
	}

	accountSet := make(map[common.Address]struct{}, len(accounts))
	for _, account := range accounts {
		accountSet[account] = struct{}{}
	}
	spendValueMap := make(map[common.Address]uint64, len(accounts))
	receiveValueMap := make(map[common.Address]uint64, len(accounts))

	signer := types.MakeSigner(s.b.ChainConfig(), block.Number(), block.Time())
	for index, tx := range block.Transactions() {
		receipt := receipts[index]
		from, err := types.Sender(signer, tx)
		if err != nil {
			return false, fmt.Errorf("get sender for tx failed: %v", err)
		}

		if _, exists := accountSet[from]; exists {
			spendValueMap[from] += receipt.GasUsed * tx.GasPrice().Uint64()
			if receipt.Status == types.ReceiptStatusSuccessful {
				spendValueMap[from] += tx.Value().Uint64()
			}
		}

		if tx.To() == nil {
			continue
		}

		if _, exists := accountSet[*tx.To()]; exists && receipt.Status == types.ReceiptStatusSuccessful {
			receiveValueMap[*tx.To()] += tx.Value().Uint64()
		}
	}

	parent, err := s.b.BlockByHash(ctx, block.ParentHash())
	if err != nil {
		return false, fmt.Errorf("block not found for block number (%d): %v", block.NumberU64()-1, err)
	}
	parentState, err := s.b.Chain().StateAt(parent.Root())
	if err != nil {
		return false, fmt.Errorf("statedb not found for block number (%d): %v", block.NumberU64()-1, err)
	}
	currentState, err := s.b.Chain().StateAt(block.Root())
	if err != nil {
		return false, fmt.Errorf("statedb not found for block number (%d): %v", block.NumberU64(), err)
	}
	for _, account := range accounts {
		parentBalance := parentState.GetBalance(account).Uint64()
		currentBalance := currentState.GetBalance(account).Uint64()
		if receiveValueMap[account]-spendValueMap[account] != currentBalance-parentBalance {
			return true, nil
		}
	}

	return false, nil
}

func (s *BlockChainAPI) replay(ctx context.Context, block *types.Block, accounts []common.Address) (*types.DiffAccountsInBlock, *state.StateDB, error) {
	result := &types.DiffAccountsInBlock{
		Number:       block.NumberU64(),
		BlockHash:    block.Hash(),
		Transactions: make([]types.DiffAccountsInTx, 0),
	}

	parent, err := s.b.BlockByHash(ctx, block.ParentHash())
	if err != nil {
		return nil, nil, fmt.Errorf("block not found for block number (%d): %v", block.NumberU64()-1, err)
	}
	statedb, err := s.b.Chain().StateAt(parent.Root())
	if err != nil {
		return nil, nil, fmt.Errorf("state not found for block number (%d): %v", block.NumberU64()-1, err)
	}

	accountSet := make(map[common.Address]struct{}, len(accounts))
	for _, account := range accounts {
		accountSet[account] = struct{}{}
	}

	// Recompute transactions.
	signer := types.MakeSigner(s.b.ChainConfig(), block.Number(), block.Time())
	for _, tx := range block.Transactions() {
		// Skip data empty tx and to is one of the interested accounts tx.
		skip := false
		if len(tx.Data()) == 0 {
			skip = true
		} else if to := tx.To(); to != nil {
			if _, exists := accountSet[*to]; exists {
				skip = true
			}
		}

		diffTx := types.DiffAccountsInTx{
			TxHash:   tx.Hash(),
			Accounts: make(map[common.Address]*big.Int, len(accounts)),
		}

		if !skip {
			// Record account balance
			for _, account := range accounts {
				diffTx.Accounts[account] = statedb.GetBalance(account).ToBig()
			}
		}

		// Apply transaction
		msg, _ := core.TransactionToMessage(tx, signer, parent.Header().BaseFee)
		txContext := core.NewEVMTxContext(msg)
		context := core.NewEVMBlockContext(block.Header(), s.b.Chain(), nil)
		vmenv := vm.NewEVM(context, txContext, statedb, s.b.ChainConfig(), vm.Config{})

		if posa, ok := s.b.Engine().(consensus.PoSA); ok {
			if isSystem, _ := posa.IsSystemTransaction(tx, block.Header()); isSystem {
				balance := statedb.GetBalance(consensus.SystemAddress)
				if balance.Cmp(common.U2560) > 0 {
					statedb.SetBalance(consensus.SystemAddress, uint256.NewInt(0))
					statedb.AddBalance(block.Header().Coinbase, balance)
				}
			}
		}

		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(tx.Gas())); err != nil {
			return nil, nil, fmt.Errorf("transaction %#x failed: %v", tx.Hash(), err)
		}
		statedb.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))

		if !skip {
			// Compute account balance diff.
			for _, account := range accounts {
				diffTx.Accounts[account] = new(big.Int).Sub(statedb.GetBalance(account).ToBig(), diffTx.Accounts[account])
				if diffTx.Accounts[account].Cmp(big.NewInt(0)) == 0 {
					delete(diffTx.Accounts, account)
				}
			}

			if len(diffTx.Accounts) != 0 {
				result.Transactions = append(result.Transactions, diffTx)
			}
		}
	}

	return result, statedb, nil
}

// GetDiffAccountsWithScope returns detailed changes of some interested accounts in a specific block number.
func (s *BlockChainAPI) GetDiffAccountsWithScope(ctx context.Context, blockNr rpc.BlockNumber, accounts []common.Address) (*types.DiffAccountsInBlock, error) {
	if s.b.Chain() == nil {
		return nil, errors.New("blockchain not support get diff accounts")
	}

	block, err := s.b.BlockByNumber(ctx, blockNr)
	if err != nil {
		return nil, fmt.Errorf("block not found for block number (%d): %v", blockNr, err)
	}

	needReplay, err := s.needToReplay(ctx, block, accounts)
	if err != nil {
		return nil, err
	}
	if !needReplay {
		return &types.DiffAccountsInBlock{
			Number:       uint64(blockNr),
			BlockHash:    block.Hash(),
			Transactions: make([]types.DiffAccountsInTx, 0),
		}, nil
	}

	result, _, err := s.replay(ctx, block, accounts)
	return result, err
}

func (s *BlockChainAPI) GetVerifyResult(ctx context.Context, blockNr rpc.BlockNumber, blockHash common.Hash, diffHash common.Hash) *core.VerifyResult {
	return s.b.Chain().GetVerifyResult(uint64(blockNr), blockHash, diffHash)
}

// RPCMarshalHeader converts the given header to the RPC output .
func RPCMarshalHeader(head *types.Header) map[string]interface{} {
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(head.Number),
		"hash":             head.Hash(),
		"parentHash":       head.ParentHash,
		"nonce":            head.Nonce,
		"mixHash":          head.MixDigest,
		"sha3Uncles":       head.UncleHash,
		"logsBloom":        head.Bloom,
		"stateRoot":        head.Root,
		"miner":            head.Coinbase,
		"difficulty":       (*hexutil.Big)(head.Difficulty),
		"extraData":        hexutil.Bytes(head.Extra),
		"gasLimit":         hexutil.Uint64(head.GasLimit),
		"gasUsed":          hexutil.Uint64(head.GasUsed),
		"timestamp":        hexutil.Uint64(head.Time),
		"transactionsRoot": head.TxHash,
		"receiptsRoot":     head.ReceiptHash,
	}
	if head.BaseFee != nil {
		result["baseFeePerGas"] = (*hexutil.Big)(head.BaseFee)
	}
	if head.WithdrawalsHash != nil {
		result["withdrawalsRoot"] = head.WithdrawalsHash
	}
	if head.BlobGasUsed != nil {
		result["blobGasUsed"] = hexutil.Uint64(*head.BlobGasUsed)
	}
	if head.ExcessBlobGas != nil {
		result["excessBlobGas"] = hexutil.Uint64(*head.ExcessBlobGas)
	}
	if head.ParentBeaconRoot != nil {
		result["parentBeaconBlockRoot"] = head.ParentBeaconRoot
	}
	return result
}

// RPCMarshalBlock converts the given block to the RPC output which depends on fullTx. If inclTx is true transactions are
// returned. When fullTx is true the returned block contains full transaction details, otherwise it will only contain
// transaction hashes.
func RPCMarshalBlock(block *types.Block, inclTx bool, fullTx bool, config *params.ChainConfig) map[string]interface{} {
	fields := RPCMarshalHeader(block.Header())
	fields["size"] = hexutil.Uint64(block.Size())

	if inclTx {
		formatTx := func(idx int, tx *types.Transaction) interface{} {
			return tx.Hash()
		}
		if fullTx {
			formatTx = func(idx int, tx *types.Transaction) interface{} {
				return newRPCTransactionFromBlockIndex(block, uint64(idx), config)
			}
		}
		txs := block.Transactions()
		transactions := make([]interface{}, len(txs))
		for i, tx := range txs {
			transactions[i] = formatTx(i, tx)
		}
		fields["transactions"] = transactions
	}
	uncles := block.Uncles()
	uncleHashes := make([]common.Hash, len(uncles))
	for i, uncle := range uncles {
		uncleHashes[i] = uncle.Hash()
	}
	fields["uncles"] = uncleHashes
	if block.Header().WithdrawalsHash != nil {
		fields["withdrawals"] = block.Withdrawals()
	}
	return fields
}

// rpcMarshalHeader uses the generalized output filler, then adds the total difficulty field, which requires
// a `BlockchainAPI`.
func (s *BlockChainAPI) rpcMarshalHeader(ctx context.Context, header *types.Header) map[string]interface{} {
	fields := RPCMarshalHeader(header)
	fields["totalDifficulty"] = (*hexutil.Big)(s.b.GetTd(ctx, header.Hash()))
	return fields
}

// rpcMarshalBlock uses the generalized output filler, then adds the total difficulty field, which requires
// a `BlockchainAPI`.
func (s *BlockChainAPI) rpcMarshalBlock(ctx context.Context, b *types.Block, inclTx bool, fullTx bool) (map[string]interface{}, error) {
	fields := RPCMarshalBlock(b, inclTx, fullTx, s.b.ChainConfig())
	if inclTx {
		fields["totalDifficulty"] = (*hexutil.Big)(s.b.GetTd(ctx, b.Hash()))
	}
	return fields, nil
}

// RPCTransaction represents a transaction that will serialize to the RPC representation of a transaction
type RPCTransaction struct {
	BlockHash           *common.Hash      `json:"blockHash"`
	BlockNumber         *hexutil.Big      `json:"blockNumber"`
	From                common.Address    `json:"from"`
	Gas                 hexutil.Uint64    `json:"gas"`
	GasPrice            *hexutil.Big      `json:"gasPrice"`
	GasFeeCap           *hexutil.Big      `json:"maxFeePerGas,omitempty"`
	GasTipCap           *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	MaxFeePerBlobGas    *hexutil.Big      `json:"maxFeePerBlobGas,omitempty"`
	Hash                common.Hash       `json:"hash"`
	Input               hexutil.Bytes     `json:"input"`
	Nonce               hexutil.Uint64    `json:"nonce"`
	To                  *common.Address   `json:"to"`
	TransactionIndex    *hexutil.Uint64   `json:"transactionIndex"`
	Value               *hexutil.Big      `json:"value"`
	Type                hexutil.Uint64    `json:"type"`
	Accesses            *types.AccessList `json:"accessList,omitempty"`
	ChainID             *hexutil.Big      `json:"chainId,omitempty"`
	BlobVersionedHashes []common.Hash     `json:"blobVersionedHashes,omitempty"`
	V                   *hexutil.Big      `json:"v"`
	R                   *hexutil.Big      `json:"r"`
	S                   *hexutil.Big      `json:"s"`
	YParity             *hexutil.Uint64   `json:"yParity,omitempty"`
}

// newRPCTransaction returns a transaction that will serialize to the RPC
// representation, with the given location metadata set (if available).
func newRPCTransaction(tx *types.Transaction, blockHash common.Hash, blockNumber uint64, blockTime uint64, index uint64, baseFee *big.Int, config *params.ChainConfig) *RPCTransaction {
	signer := types.MakeSigner(config, new(big.Int).SetUint64(blockNumber), blockTime)
	from, _ := types.Sender(signer, tx)
	v, r, s := tx.RawSignatureValues()
	result := &RPCTransaction{
		Type:     hexutil.Uint64(tx.Type()),
		From:     from,
		Gas:      hexutil.Uint64(tx.Gas()),
		GasPrice: (*hexutil.Big)(tx.GasPrice()),
		Hash:     tx.Hash(),
		Input:    hexutil.Bytes(tx.Data()),
		Nonce:    hexutil.Uint64(tx.Nonce()),
		To:       tx.To(),
		Value:    (*hexutil.Big)(tx.Value()),
		V:        (*hexutil.Big)(v),
		R:        (*hexutil.Big)(r),
		S:        (*hexutil.Big)(s),
	}
	if blockHash != (common.Hash{}) {
		result.BlockHash = &blockHash
		result.BlockNumber = (*hexutil.Big)(new(big.Int).SetUint64(blockNumber))
		result.TransactionIndex = (*hexutil.Uint64)(&index)
	}

	switch tx.Type() {
	case types.LegacyTxType:
		// if a legacy transaction has an EIP-155 chain id, include it explicitly
		if id := tx.ChainId(); id.Sign() != 0 {
			result.ChainID = (*hexutil.Big)(id)
		}

	case types.AccessListTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity

	case types.DynamicFeeTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		// if the transaction has been mined, compute the effective gas price
		if baseFee != nil && blockHash != (common.Hash{}) {
			// price = min(gasTipCap + baseFee, gasFeeCap)
			result.GasPrice = (*hexutil.Big)(effectiveGasPrice(tx, baseFee))
		} else {
			result.GasPrice = (*hexutil.Big)(tx.GasFeeCap())
		}

	case types.BlobTxType:
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.YParity = &yparity
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		// if the transaction has been mined, compute the effective gas price
		if baseFee != nil && blockHash != (common.Hash{}) {
			result.GasPrice = (*hexutil.Big)(effectiveGasPrice(tx, baseFee))
		} else {
			result.GasPrice = (*hexutil.Big)(tx.GasFeeCap())
		}
		result.MaxFeePerBlobGas = (*hexutil.Big)(tx.BlobGasFeeCap())
		result.BlobVersionedHashes = tx.BlobHashes()
	}
	return result
}

// effectiveGasPrice computes the transaction gas fee, based on the given basefee value.
//
//	price = min(gasTipCap + baseFee, gasFeeCap)
func effectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	fee := tx.GasTipCap()
	fee = fee.Add(fee, baseFee)
	if tx.GasFeeCapIntCmp(fee) < 0 {
		return tx.GasFeeCap()
	}
	return fee
}

// NewRPCPendingTransaction returns a pending transaction that will serialize to the RPC representation
func NewRPCPendingTransaction(tx *types.Transaction, current *types.Header, config *params.ChainConfig) *RPCTransaction {
	var (
		baseFee     *big.Int
		blockNumber = uint64(0)
		blockTime   = uint64(0)
	)
	if current != nil {
		baseFee = eip1559.CalcBaseFee(config, current)
		blockNumber = current.Number.Uint64()
		blockTime = current.Time
	}
	return newRPCTransaction(tx, common.Hash{}, blockNumber, blockTime, 0, baseFee, config)
}

// newRPCTransactionsFromBlockIndex returns transactions that will serialize to the RPC representation.
func newRPCTransactionsFromBlockIndex(b *types.Block, config *params.ChainConfig) []*RPCTransaction {
	txs := b.Transactions()
	result := make([]*RPCTransaction, 0, len(txs))

	for idx, tx := range txs {
		result = append(result, newRPCTransaction(tx, b.Hash(), b.NumberU64(), b.Time(), uint64(idx), b.BaseFee(), config))
	}
	return result
}

// newRPCTransactionFromBlockIndex returns a transaction that will serialize to the RPC representation.
func newRPCTransactionFromBlockIndex(b *types.Block, index uint64, config *params.ChainConfig) *RPCTransaction {
	txs := b.Transactions()
	if index >= uint64(len(txs)) {
		return nil
	}
	return newRPCTransaction(txs[index], b.Hash(), b.NumberU64(), b.Time(), index, b.BaseFee(), config)
}

// newRPCRawTransactionFromBlockIndex returns the bytes of a transaction given a block and a transaction index.
func newRPCRawTransactionFromBlockIndex(b *types.Block, index uint64) hexutil.Bytes {
	txs := b.Transactions()
	if index >= uint64(len(txs)) {
		return nil
	}
	blob, _ := txs[index].MarshalBinary()
	return blob
}

// accessListResult returns an optional accesslist
// It's the result of the `debug_createAccessList` RPC call.
// It contains an error if the transaction itself failed.
type accessListResult struct {
	Accesslist *types.AccessList `json:"accessList"`
	Error      string            `json:"error,omitempty"`
	GasUsed    hexutil.Uint64    `json:"gasUsed"`
}

// CreateAccessList creates an EIP-2930 type AccessList for the given transaction.
// Reexec and BlockNrOrHash can be specified to create the accessList on top of a certain state.
func (s *BlockChainAPI) CreateAccessList(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (*accessListResult, error) {
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	acl, gasUsed, vmerr, err := AccessList(ctx, s.b, bNrOrHash, args)
	if err != nil {
		return nil, err
	}
	result := &accessListResult{Accesslist: &acl, GasUsed: hexutil.Uint64(gasUsed)}
	if vmerr != nil {
		result.Error = vmerr.Error()
	}
	return result, nil
}

// AccessList creates an access list for the given transaction.
// If the accesslist creation fails an error is returned.
// If the transaction itself fails, an vmErr is returned.
func AccessList(ctx context.Context, b Backend, blockNrOrHash rpc.BlockNumberOrHash, args TransactionArgs) (acl types.AccessList, gasUsed uint64, vmErr error, err error) {
	// Retrieve the execution context
	db, header, err := b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if db == nil || err != nil {
		return nil, 0, nil, err
	}

	// Ensure any missing fields are filled, extract the recipient and input data
	if err := args.setDefaults(ctx, b, true); err != nil {
		return nil, 0, nil, err
	}
	var to common.Address
	if args.To != nil {
		to = *args.To
	} else {
		to = crypto.CreateAddress(args.from(), uint64(*args.Nonce))
	}
	isPostMerge := header.Difficulty.Cmp(common.Big0) == 0
	// Retrieve the precompiles since they don't need to be added to the access list
	precompiles := vm.ActivePrecompiles(b.ChainConfig().Rules(header.Number, isPostMerge, header.Time))

	// Create an initial tracer
	prevTracer := logger.NewAccessListTracer(nil, args.from(), to, precompiles)
	if args.AccessList != nil {
		prevTracer = logger.NewAccessListTracer(*args.AccessList, args.from(), to, precompiles)
	}
	for {
		// Retrieve the current access list to expand
		accessList := prevTracer.AccessList()
		log.Trace("Creating access list", "input", accessList)

		// Copy the original db so we don't modify it
		statedb := db.Copy()
		// Set the accesslist to the last al
		args.AccessList = &accessList
		msg, err := args.ToMessage(b.RPCGasCap(), header.BaseFee)
		if err != nil {
			return nil, 0, nil, err
		}

		// Apply the transaction with the access list tracer
		tracer := logger.NewAccessListTracer(accessList, args.from(), to, precompiles)
		config := vm.Config{Tracer: tracer, NoBaseFee: true}
		vmenv := b.GetEVM(ctx, msg, statedb, header, &config, nil)
		res, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.GasLimit))
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to apply transaction: %v err: %v", args.toTransaction().Hash(), err)
		}
		if tracer.Equal(prevTracer) {
			return accessList, res.UsedGas, res.Err, nil
		}
		prevTracer = tracer
	}
}

// TransactionAPI exposes methods for reading and creating transaction data.
type TransactionAPI struct {
	b         Backend
	nonceLock *AddrLocker
	signer    types.Signer
}

// NewTransactionAPI creates a new RPC service with methods for interacting with transactions.
func NewTransactionAPI(b Backend, nonceLock *AddrLocker) *TransactionAPI {
	// The signer used by the API should always be the 'latest' known one because we expect
	// signers to be backwards-compatible with old transactions.
	signer := types.LatestSigner(b.ChainConfig())
	return &TransactionAPI{b, nonceLock, signer}
}

// GetBlockTransactionCountByNumber returns the number of transactions in the block with the given block number.
func (s *TransactionAPI) GetBlockTransactionCountByNumber(ctx context.Context, blockNr rpc.BlockNumber) *hexutil.Uint {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

// GetBlockTransactionCountByHash returns the number of transactions in the block with the given hash.
func (s *TransactionAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) *hexutil.Uint {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

// GetTransactionsByBlockNumber returns all the transactions for the given block number.
func (s *TransactionAPI) GetTransactionsByBlockNumber(ctx context.Context, blockNr rpc.BlockNumber) []*RPCTransaction {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCTransactionsFromBlockIndex(block, s.b.ChainConfig())
	}
	return nil
}

// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (s *TransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index), s.b.ChainConfig())
	}
	return nil
}

// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (s *TransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index), s.b.ChainConfig())
	}
	return nil
}

// GetRawTransactionByBlockNumberAndIndex returns the bytes of the transaction for the given block number and index.
func (s *TransactionAPI) GetRawTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

// GetRawTransactionByBlockHashAndIndex returns the bytes of the transaction for the given block hash and index.
func (s *TransactionAPI) GetRawTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (s *TransactionAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	// Ask transaction pool for the nonce which includes pending transactions
	if blockNr, ok := blockNrOrHash.Number(); ok && blockNr == rpc.PendingBlockNumber {
		nonce, err := s.b.GetPoolNonce(ctx, address)
		if err != nil {
			return nil, err
		}
		return (*hexutil.Uint64)(&nonce), nil
	}
	// Resolve block number and use its state to ask for the nonce
	state, _, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	nonce := state.GetNonce(address)
	return (*hexutil.Uint64)(&nonce), state.Error()
}

// GetTransactionByHash returns the transaction for the given hash
func (s *TransactionAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	// Try to return an already finalized transaction
	found, tx, blockHash, blockNumber, index, err := s.b.GetTransaction(ctx, hash)
	if !found {
		// No finalized transaction, try to retrieve it from the pool
		if tx := s.b.GetPoolTransaction(hash); tx != nil {
			return NewRPCPendingTransaction(tx, s.b.CurrentHeader(), s.b.ChainConfig()), nil
		}
		if err == nil {
			return nil, nil
		}
		return nil, NewTxIndexingError()
	}
	header, err := s.b.HeaderByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	return newRPCTransaction(tx, blockHash, blockNumber, header.Time, index, header.BaseFee, s.b.ChainConfig()), nil
}

// GetRawTransactionByHash returns the bytes of the transaction for the given hash.
func (s *TransactionAPI) GetRawTransactionByHash(ctx context.Context, hash common.Hash) (hexutil.Bytes, error) {
	// Retrieve a finalized transaction, or a pooled otherwise
	found, tx, _, _, _, err := s.b.GetTransaction(ctx, hash)
	if !found {
		if tx = s.b.GetPoolTransaction(hash); tx != nil {
			return tx.MarshalBinary()
		}
		if err == nil {
			return nil, nil
		}
		return nil, NewTxIndexingError()
	}
	return tx.MarshalBinary()
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (s *TransactionAPI) GetTransactionReceiptsByBlockNumber(ctx context.Context, blockNr rpc.BlockNumber) ([]map[string]interface{}, error) {
	blockNumber := uint64(blockNr.Int64())
	blockHash := rawdb.ReadCanonicalHash(s.b.ChainDb(), blockNumber)

	receipts, err := s.b.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if receipts == nil {
		return nil, fmt.Errorf("block %d receipts not found", blockNumber)
	}
	block, err := s.b.BlockByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block %d not found", blockNumber)
	}
	txs := block.Transactions()
	if len(txs) != len(receipts) {
		return nil, errors.New("txs length doesn't equal to receipts' length")
	}

	txReceipts := make([]map[string]interface{}, 0, len(txs))
	for idx, receipt := range receipts {
		tx := txs[idx]
		signer := types.MakeSigner(s.b.ChainConfig(), block.Number(), block.Time())
		from, _ := types.Sender(signer, tx)

		fields := map[string]interface{}{
			"blockHash":         blockHash,
			"blockNumber":       hexutil.Uint64(blockNumber),
			"transactionHash":   tx.Hash(),
			"transactionIndex":  hexutil.Uint64(idx),
			"from":              from,
			"to":                tx.To(),
			"gasUsed":           hexutil.Uint64(receipt.GasUsed),
			"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
			"contractAddress":   nil,
			"logs":              receipt.Logs,
			"logsBloom":         receipt.Bloom,
			"type":              hexutil.Uint(tx.Type()),
			"effectiveGasPrice": (*hexutil.Big)(receipt.EffectiveGasPrice),
		}

		// Assign receipt status or post state.
		if len(receipt.PostState) > 0 {
			fields["root"] = hexutil.Bytes(receipt.PostState)
		} else {
			fields["status"] = hexutil.Uint(receipt.Status)
		}
		if receipt.Logs == nil {
			fields["logs"] = []*types.Log{}
		}
		// If the ContractAddress is 20 0x0 bytes, assume it is not a contract creation
		if receipt.ContractAddress != (common.Address{}) {
			fields["contractAddress"] = receipt.ContractAddress
		}

		txReceipts = append(txReceipts, fields)
	}

	return txReceipts, nil
}

// GetTransactionDataAndReceipt returns the original transaction data and transaction receipt for the given transaction hash.
func (s *TransactionAPI) GetTransactionDataAndReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(s.b.ChainDb(), hash)
	if tx == nil {
		return nil, nil
	}
	receipts, err := s.b.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if len(receipts) <= int(index) {
		return nil, nil
	}
	receipt := receipts[index]

	// Derive the sender.
	header, err := s.b.HeaderByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	signer := types.MakeSigner(s.b.ChainConfig(), header.Number, header.Time)
	fields := marshalReceipt(receipt, blockHash, blockNumber, signer, tx, int(index))

	// TODO use nil basefee before landon fork is enabled
	rpcTransaction := newRPCTransaction(tx, blockHash, blockNumber, header.Time, index, nil, s.b.ChainConfig())
	txData := map[string]interface{}{
		"blockHash":        rpcTransaction.BlockHash.String(),
		"blockNumber":      rpcTransaction.BlockNumber.String(),
		"from":             rpcTransaction.From.String(),
		"gas":              rpcTransaction.Gas.String(),
		"gasPrice":         rpcTransaction.GasPrice.String(),
		"hash":             rpcTransaction.Hash.String(),
		"input":            rpcTransaction.Input.String(),
		"nonce":            rpcTransaction.Nonce.String(),
		"to":               rpcTransaction.To.String(),
		"transactionIndex": rpcTransaction.TransactionIndex.String(),
		"value":            rpcTransaction.Value.String(),
		"v":                rpcTransaction.V.String(),
		"r":                rpcTransaction.R.String(),
		"s":                rpcTransaction.S.String(),
	}

	result := map[string]interface{}{
		"txData":  txData,
		"receipt": fields,
	}
	return result, nil
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (s *TransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	found, tx, blockHash, blockNumber, index, err := s.b.GetTransaction(ctx, hash)
	if err != nil {
		return nil, NewTxIndexingError() // transaction is not fully indexed
	}
	if !found {
		return nil, nil // transaction is not existent or reachable
	}
	header, err := s.b.HeaderByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	receipts, err := s.b.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if uint64(len(receipts)) <= index {
		return nil, nil
	}
	receipt := receipts[index]

	// Derive the sender.
	signer := types.MakeSigner(s.b.ChainConfig(), header.Number, header.Time)
	return marshalReceipt(receipt, blockHash, blockNumber, signer, tx, int(index)), nil
}

// marshalReceipt marshals a transaction receipt into a JSON object.
func marshalReceipt(receipt *types.Receipt, blockHash common.Hash, blockNumber uint64, signer types.Signer, tx *types.Transaction, txIndex int) map[string]interface{} {
	from, _ := types.Sender(signer, tx)

	fields := map[string]interface{}{
		"blockHash":         blockHash,
		"blockNumber":       hexutil.Uint64(blockNumber),
		"transactionHash":   tx.Hash(),
		"transactionIndex":  hexutil.Uint64(txIndex),
		"from":              from,
		"to":                tx.To(),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"contractAddress":   nil,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
		"type":              hexutil.Uint(tx.Type()),
		"effectiveGasPrice": (*hexutil.Big)(receipt.EffectiveGasPrice),
	}

	// Assign receipt status or post state.
	if len(receipt.PostState) > 0 {
		fields["root"] = hexutil.Bytes(receipt.PostState)
	} else {
		fields["status"] = hexutil.Uint(receipt.Status)
	}
	if receipt.Logs == nil {
		fields["logs"] = []*types.Log{}
	}

	if tx.Type() == types.BlobTxType {
		fields["blobGasUsed"] = hexutil.Uint64(receipt.BlobGasUsed)
		fields["blobGasPrice"] = (*hexutil.Big)(receipt.BlobGasPrice)
	}

	// If the ContractAddress is 20 0x0 bytes, assume it is not a contract creation
	if receipt.ContractAddress != (common.Address{}) {
		fields["contractAddress"] = receipt.ContractAddress
	}
	return fields
}

func marshalBlobSidecar(sidecar *types.BlobSidecar, fullBlob bool) map[string]interface{} {
	fields := map[string]interface{}{
		"blockHash":   sidecar.BlockHash,
		"blockNumber": hexutil.EncodeUint64(sidecar.BlockNumber.Uint64()),
		"txHash":      sidecar.TxHash,
		"txIndex":     hexutil.EncodeUint64(sidecar.TxIndex),
	}
	fields["blobSidecar"] = marshalBlob(sidecar.BlobTxSidecar, fullBlob)
	return fields
}

func marshalBlob(blobTxSidecar types.BlobTxSidecar, fullBlob bool) map[string]interface{} {
	fields := map[string]interface{}{
		"blobs":       blobTxSidecar.Blobs,
		"commitments": blobTxSidecar.Commitments,
		"proofs":      blobTxSidecar.Proofs,
	}
	if !fullBlob {
		var blobs []common.Hash
		for _, blob := range blobTxSidecar.Blobs {
			var value common.Hash
			copy(value[:], blob[:32])
			blobs = append(blobs, value)
		}
		fields["blobs"] = blobs
	}
	return fields
}

// sign is a helper function that signs a transaction with the private key of the given address.
func (s *TransactionAPI) sign(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
	// Look up the wallet containing the requested signer
	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}
	// Request the wallet to sign the transaction
	return wallet.SignTx(account, tx, s.b.ChainConfig().ChainID)
}

// SubmitTransaction is a helper function that submits tx to txPool and logs a message.
func SubmitTransaction(ctx context.Context, b Backend, tx *types.Transaction) (common.Hash, error) {
	// If the transaction fee cap is already specified, ensure the
	// fee of the given transaction is _reasonable_.
	if err := checkTxFee(tx.GasPrice(), tx.Gas(), b.RPCTxFeeCap()); err != nil {
		return common.Hash{}, err
	}
	if !b.UnprotectedAllowed() && !tx.Protected() {
		// Ensure only eip155 signed transactions are submitted if EIP155Required is set.
		return common.Hash{}, errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
	}
	if err := b.SendTx(ctx, tx); err != nil {
		return common.Hash{}, err
	}
	// Print a log with full tx details for manual investigations and interventions
	head := b.CurrentBlock()
	signer := types.MakeSigner(b.ChainConfig(), head.Number, head.Time)
	from, err := types.Sender(signer, tx)
	if err != nil {
		return common.Hash{}, err
	}
	xForward := ctx.Value("X-Forwarded-For")

	if tx.To() == nil {
		addr := crypto.CreateAddress(from, tx.Nonce())
		log.Info("Submitted contract creation", "hash", tx.Hash().Hex(), "from", from, "nonce", tx.Nonce(), "contract", addr.Hex(), "value", tx.Value(), "x-forward-ip", xForward)
	} else {
		log.Info("Submitted transaction", "hash", tx.Hash().Hex(), "from", from, "nonce", tx.Nonce(), "recipient", tx.To(), "value", tx.Value(), "x-forward-ip", xForward)
	}
	return tx.Hash(), nil
}

// SendTransaction creates a transaction for the given argument, sign it and submit it to the
// transaction pool.
func (s *TransactionAPI) SendTransaction(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	// Look up the wallet containing the requested signer
	account := accounts.Account{Address: args.from()}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return common.Hash{}, err
	}

	if args.Nonce == nil {
		// Hold the mutex around signing to prevent concurrent assignment of
		// the same nonce to multiple accounts.
		s.nonceLock.LockAddr(args.from())
		defer s.nonceLock.UnlockAddr(args.from())
	}
	if args.IsEIP4844() {
		return common.Hash{}, errBlobTxNotSupported
	}

	// Set some sanity defaults and terminate on failure
	if err := args.setDefaults(ctx, s.b, false); err != nil {
		return common.Hash{}, err
	}
	// Assemble the transaction and sign with the wallet
	tx := args.toTransaction()

	signed, err := wallet.SignTx(account, tx, s.b.ChainConfig().ChainID)
	if err != nil {
		return common.Hash{}, err
	}
	return SubmitTransaction(ctx, s.b, signed)
}

// FillTransaction fills the defaults (nonce, gas, gasPrice or 1559 fields)
// on a given unsigned transaction, and returns it to the caller for further
// processing (signing + broadcast).
func (s *TransactionAPI) FillTransaction(ctx context.Context, args TransactionArgs) (*SignTransactionResult, error) {
	args.blobSidecarAllowed = true

	// Set some sanity defaults and terminate on failure
	if err := args.setDefaults(ctx, s.b, false); err != nil {
		return nil, err
	}
	// Assemble the transaction and obtain rlp
	tx := args.toTransaction()
	data, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &SignTransactionResult{data, tx}, nil
}

// SendRawTransaction will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce.
func (s *TransactionAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	return SubmitTransaction(ctx, s.b, tx)
}

// SendRawTransactionConditional will add the signed transaction to the transaction pool.
// The sender/bundler is responsible for signing the transaction
func (s *TransactionAPI) SendRawTransactionConditional(ctx context.Context, input hexutil.Bytes, opts TransactionOpts) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	header := s.b.CurrentHeader()
	state, _, err := s.b.StateAndHeaderByNumber(ctx, rpc.BlockNumber(header.Number.Int64()))
	if state == nil || err != nil {
		return common.Hash{}, err
	}
	if err := opts.Check(header.Number.Uint64(), header.Time, state); err != nil {
		return common.Hash{}, err
	}
	return SubmitTransaction(ctx, s.b, tx)
}

// Sign calculates an ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message).
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The account associated with addr must be unlocked.
//
// https://github.com/ethereum/wiki/wiki/JSON-RPC#eth_sign
func (s *TransactionAPI) Sign(addr common.Address, data hexutil.Bytes) (hexutil.Bytes, error) {
	// Look up the wallet containing the requested signer
	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}
	// Sign the requested hash with the wallet
	signature, err := wallet.SignText(account, data)
	if err == nil {
		signature[64] += 27 // Transform V from 0/1 to 27/28 according to the yellow paper
	}
	return signature, err
}

// SignTransactionResult represents a RLP encoded signed transaction.
type SignTransactionResult struct {
	Raw hexutil.Bytes      `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (s *TransactionAPI) SignTransaction(ctx context.Context, args TransactionArgs) (*SignTransactionResult, error) {
	if args.Gas == nil {
		return nil, errors.New("gas not specified")
	}
	if args.GasPrice == nil && (args.MaxPriorityFeePerGas == nil || args.MaxFeePerGas == nil) {
		return nil, errors.New("missing gasPrice or maxFeePerGas/maxPriorityFeePerGas")
	}
	if args.IsEIP4844() {
		return nil, errBlobTxNotSupported
	}
	if args.Nonce == nil {
		return nil, errors.New("nonce not specified")
	}
	if err := args.setDefaults(ctx, s.b, false); err != nil {
		return nil, err
	}
	// Before actually sign the transaction, ensure the transaction fee is reasonable.
	tx := args.toTransaction()
	if err := checkTxFee(tx.GasPrice(), tx.Gas(), s.b.RPCTxFeeCap()); err != nil {
		return nil, err
	}
	signed, err := s.sign(args.from(), tx)
	if err != nil {
		return nil, err
	}
	data, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &SignTransactionResult{data, signed}, nil
}

// PendingTransactions returns the transactions that are in the transaction pool
// and have a from address that is one of the accounts this node manages.
func (s *TransactionAPI) PendingTransactions() ([]*RPCTransaction, error) {
	pending, err := s.b.GetPoolTransactions()
	if err != nil {
		return nil, err
	}
	accounts := make(map[common.Address]struct{})
	for _, wallet := range s.b.AccountManager().Wallets() {
		for _, account := range wallet.Accounts() {
			accounts[account.Address] = struct{}{}
		}
	}
	curHeader := s.b.CurrentHeader()
	transactions := make([]*RPCTransaction, 0, len(pending))
	for _, tx := range pending {
		from, _ := types.Sender(s.signer, tx)
		if _, exists := accounts[from]; exists {
			transactions = append(transactions, NewRPCPendingTransaction(tx, curHeader, s.b.ChainConfig()))
		}
	}
	return transactions, nil
}

// Resend accepts an existing transaction and a new gas price and limit. It will remove
// the given transaction from the pool and reinsert it with the new gas price and limit.
func (s *TransactionAPI) Resend(ctx context.Context, sendArgs TransactionArgs, gasPrice *hexutil.Big, gasLimit *hexutil.Uint64) (common.Hash, error) {
	if sendArgs.Nonce == nil {
		return common.Hash{}, errors.New("missing transaction nonce in transaction spec")
	}
	if err := sendArgs.setDefaults(ctx, s.b, false); err != nil {
		return common.Hash{}, err
	}
	matchTx := sendArgs.toTransaction()

	// Before replacing the old transaction, ensure the _new_ transaction fee is reasonable.
	var price = matchTx.GasPrice()
	if gasPrice != nil {
		price = gasPrice.ToInt()
	}
	var gas = matchTx.Gas()
	if gasLimit != nil {
		gas = uint64(*gasLimit)
	}
	if err := checkTxFee(price, gas, s.b.RPCTxFeeCap()); err != nil {
		return common.Hash{}, err
	}
	// Iterate the pending list for replacement
	pending, err := s.b.GetPoolTransactions()
	if err != nil {
		return common.Hash{}, err
	}
	for _, p := range pending {
		wantSigHash := s.signer.Hash(matchTx)
		pFrom, err := types.Sender(s.signer, p)
		if err == nil && pFrom == sendArgs.from() && s.signer.Hash(p) == wantSigHash {
			// Match. Re-sign and send the transaction.
			if gasPrice != nil && (*big.Int)(gasPrice).Sign() != 0 {
				sendArgs.GasPrice = gasPrice
			}
			if gasLimit != nil && *gasLimit != 0 {
				sendArgs.Gas = gasLimit
			}
			signedTx, err := s.sign(sendArgs.from(), sendArgs.toTransaction())
			if err != nil {
				return common.Hash{}, err
			}
			if err = s.b.SendTx(ctx, signedTx); err != nil {
				return common.Hash{}, err
			}
			return signedTx.Hash(), nil
		}
	}
	return common.Hash{}, fmt.Errorf("transaction %#x not found", matchTx.Hash())
}

// DebugAPI is the collection of Ethereum APIs exposed over the debugging
// namespace.
type DebugAPI struct {
	b Backend
}

// NewDebugAPI creates a new instance of DebugAPI.
func NewDebugAPI(b Backend) *DebugAPI {
	return &DebugAPI{b: b}
}

// GetRawHeader retrieves the RLP encoding for a single header.
func (api *DebugAPI) GetRawHeader(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	var hash common.Hash
	if h, ok := blockNrOrHash.Hash(); ok {
		hash = h
	} else {
		block, err := api.b.BlockByNumberOrHash(ctx, blockNrOrHash)
		if err != nil {
			return nil, err
		}
		hash = block.Hash()
	}
	header, _ := api.b.HeaderByHash(ctx, hash)
	if header == nil {
		return nil, fmt.Errorf("header #%d not found", hash)
	}
	return rlp.EncodeToBytes(header)
}

// GetRawBlock retrieves the RLP encoded for a single block.
func (api *DebugAPI) GetRawBlock(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	var hash common.Hash
	if h, ok := blockNrOrHash.Hash(); ok {
		hash = h
	} else {
		block, err := api.b.BlockByNumberOrHash(ctx, blockNrOrHash)
		if err != nil {
			return nil, err
		}
		hash = block.Hash()
	}
	block, _ := api.b.BlockByHash(ctx, hash)
	if block == nil {
		return nil, fmt.Errorf("block #%d not found", hash)
	}
	return rlp.EncodeToBytes(block)
}

// GetRawReceipts retrieves the binary-encoded receipts of a single block.
func (api *DebugAPI) GetRawReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]hexutil.Bytes, error) {
	var hash common.Hash
	if h, ok := blockNrOrHash.Hash(); ok {
		hash = h
	} else {
		block, err := api.b.BlockByNumberOrHash(ctx, blockNrOrHash)
		if err != nil {
			return nil, err
		}
		hash = block.Hash()
	}
	receipts, err := api.b.GetReceipts(ctx, hash)
	if err != nil {
		return nil, err
	}
	result := make([]hexutil.Bytes, len(receipts))
	for i, receipt := range receipts {
		b, err := receipt.MarshalBinary()
		if err != nil {
			return nil, err
		}
		result[i] = b
	}
	return result, nil
}

// GetRawTransaction returns the bytes of the transaction for the given hash.
func (s *DebugAPI) GetRawTransaction(ctx context.Context, hash common.Hash) (hexutil.Bytes, error) {
	// Retrieve a finalized transaction, or a pooled otherwise
	found, tx, _, _, _, err := s.b.GetTransaction(ctx, hash)
	if !found {
		if tx = s.b.GetPoolTransaction(hash); tx != nil {
			return tx.MarshalBinary()
		}
		if err == nil {
			return nil, nil
		}
		return nil, NewTxIndexingError()
	}
	return tx.MarshalBinary()
}

// PrintBlock retrieves a block and returns its pretty printed form.
func (api *DebugAPI) PrintBlock(ctx context.Context, number uint64) (string, error) {
	block, _ := api.b.BlockByNumber(ctx, rpc.BlockNumber(number))
	if block == nil {
		return "", fmt.Errorf("block #%d not found", number)
	}
	return spew.Sdump(block), nil
}

// ChaindbProperty returns leveldb properties of the key-value database.
func (api *DebugAPI) ChaindbProperty(property string) (string, error) {
	return api.b.ChainDb().Stat(property)
}

// ChaindbCompact flattens the entire key-value database into a single level,
// removing all unused slots and merging all keys.
func (api *DebugAPI) ChaindbCompact() error {
	cstart := time.Now()
	for b := 0; b <= 255; b++ {
		var (
			start = []byte{byte(b)}
			end   = []byte{byte(b + 1)}
		)
		if b == 255 {
			end = nil
		}
		log.Info("Compacting database", "range", fmt.Sprintf("%#X-%#X", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
		if err := api.b.ChainDb().Compact(start, end); err != nil {
			log.Error("Database compaction failed", "err", err)
			return err
		}
	}
	return nil
}

// SetHead rewinds the head of the blockchain to a previous block.
func (api *DebugAPI) SetHead(number hexutil.Uint64) {
	api.b.SetHead(uint64(number))
}

// NetAPI offers network related RPC methods
type NetAPI struct {
	net            *p2p.Server
	networkVersion uint64
}

// NewNetAPI creates a new net API instance.
func NewNetAPI(net *p2p.Server, networkVersion uint64) *NetAPI {
	return &NetAPI{net, networkVersion}
}

// Listening returns an indication if the node is listening for network connections.
func (s *NetAPI) Listening() bool {
	return true // always listening
}

// PeerCount returns the number of connected peers
func (s *NetAPI) PeerCount() hexutil.Uint {
	return hexutil.Uint(s.net.PeerCount())
}

// Version returns the current ethereum protocol version.
func (s *NetAPI) Version() string {
	return fmt.Sprintf("%d", s.networkVersion)
}

// NodeInfo retrieves all the information we know about the host node at the
// protocol granularity. This is the same as the `admin_nodeInfo` method.
func (s *NetAPI) NodeInfo() (*p2p.NodeInfo, error) {
	server := s.net
	if server == nil {
		return nil, errors.New("server not found")
	}
	return s.net.NodeInfo(), nil
}

// checkTxFee is an internal function used to check whether the fee of
// the given transaction is _reasonable_(under the cap).
func checkTxFee(gasPrice *big.Int, gas uint64, cap float64) error {
	// Short circuit if there is no cap for transaction fee at all.
	if cap == 0 {
		return nil
	}
	feeEth := new(big.Float).Quo(new(big.Float).SetInt(new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(gas))), new(big.Float).SetInt(big.NewInt(params.Ether)))
	feeFloat, _ := feeEth.Float64()
	if feeFloat > cap {
		return fmt.Errorf("tx fee (%.2f ether) exceeds the configured cap (%.2f ether)", feeFloat, cap)
	}
	return nil
}
