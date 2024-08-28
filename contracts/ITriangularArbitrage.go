// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package triangulararbitrage

import (
	"errors"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// ITriangularArbitrageTriangular is an auto generated low-level Go binding around an user-defined struct.
type ITriangularArbitrageTriangular struct {
	Token0  common.Address
	Router0 common.Address
	Pair0   common.Address
	Token1  common.Address
	Router1 common.Address
	Pair1   common.Address
	Token2  common.Address
	Router2 common.Address
	Pair2   common.Address
}

// TriangulararbitrageMetaData contains all meta data concerning the Triangulararbitrage contract.
var TriangulararbitrageMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"name\":\"arb_wcnwzblucpyf\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"token0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair2\",\"type\":\"address\"}],\"internalType\":\"structITriangularArbitrage.Triangular\",\"name\":\"t\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"startRatio\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"endRatio\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"pieces\",\"type\":\"uint256\"}],\"name\":\"arbitrageQuery\",\"outputs\":[{\"internalType\":\"int256[]\",\"name\":\"roi\",\"type\":\"int256[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"token0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair2\",\"type\":\"address\"}],\"internalType\":\"structITriangularArbitrage.Triangular\",\"name\":\"t\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"threshold\",\"type\":\"uint256\"}],\"name\":\"isTriangularValid\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// TriangulararbitrageABI is the input ABI used to generate the binding from.
// Deprecated: Use TriangulararbitrageMetaData.ABI instead.
var TriangulararbitrageABI = TriangulararbitrageMetaData.ABI

// Triangulararbitrage is an auto generated Go binding around an Ethereum contract.
type Triangulararbitrage struct {
	TriangulararbitrageCaller     // Read-only binding to the contract
	TriangulararbitrageTransactor // Write-only binding to the contract
	TriangulararbitrageFilterer   // Log filterer for contract events
}

// TriangulararbitrageCaller is an auto generated read-only Go binding around an Ethereum contract.
type TriangulararbitrageCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TriangulararbitrageTransactor is an auto generated write-only Go binding around an Ethereum contract.
type TriangulararbitrageTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TriangulararbitrageFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type TriangulararbitrageFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TriangulararbitrageSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type TriangulararbitrageSession struct {
	Contract     *Triangulararbitrage // Generic contract binding to set the session for
	CallOpts     bind.CallOpts        // Call options to use throughout this session
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// TriangulararbitrageCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type TriangulararbitrageCallerSession struct {
	Contract *TriangulararbitrageCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts              // Call options to use throughout this session
}

// TriangulararbitrageTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type TriangulararbitrageTransactorSession struct {
	Contract     *TriangulararbitrageTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts              // Transaction auth options to use throughout this session
}

// TriangulararbitrageRaw is an auto generated low-level Go binding around an Ethereum contract.
type TriangulararbitrageRaw struct {
	Contract *Triangulararbitrage // Generic contract binding to access the raw methods on
}

// TriangulararbitrageCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type TriangulararbitrageCallerRaw struct {
	Contract *TriangulararbitrageCaller // Generic read-only contract binding to access the raw methods on
}

// TriangulararbitrageTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type TriangulararbitrageTransactorRaw struct {
	Contract *TriangulararbitrageTransactor // Generic write-only contract binding to access the raw methods on
}

var triangulararbitrage *Triangulararbitrage

func GetTriangulararbitrage() *Triangulararbitrage {
	if triangulararbitrage == nil {
		conn, err := ethclient.Dial("/blockchain/bsc/build/bin/node/geth.ipc")
		if err != nil {
			log.Info("Failed to connect to the local Ethereum client，error", err)
			return nil
		}
		triangulararbitrage, err = NewTriangulararbitrage(common.HexToAddress("0x123456"), conn)
		if err != nil {
			log.Info("Failed to create triangulararbitrage instance，error", err)
			return nil
		}
	}
	return triangulararbitrage
}

// NewTriangulararbitrage creates a new instance of Triangulararbitrage, bound to a specific deployed contract.
func NewTriangulararbitrage(address common.Address, backend bind.ContractBackend) (*Triangulararbitrage, error) {
	contract, err := bindTriangulararbitrage(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Triangulararbitrage{TriangulararbitrageCaller: TriangulararbitrageCaller{contract: contract}, TriangulararbitrageTransactor: TriangulararbitrageTransactor{contract: contract}, TriangulararbitrageFilterer: TriangulararbitrageFilterer{contract: contract}}, nil
}

// NewTriangulararbitrageCaller creates a new read-only instance of Triangulararbitrage, bound to a specific deployed contract.
func NewTriangulararbitrageCaller(address common.Address, caller bind.ContractCaller) (*TriangulararbitrageCaller, error) {
	contract, err := bindTriangulararbitrage(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &TriangulararbitrageCaller{contract: contract}, nil
}

// NewTriangulararbitrageTransactor creates a new write-only instance of Triangulararbitrage, bound to a specific deployed contract.
func NewTriangulararbitrageTransactor(address common.Address, transactor bind.ContractTransactor) (*TriangulararbitrageTransactor, error) {
	contract, err := bindTriangulararbitrage(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &TriangulararbitrageTransactor{contract: contract}, nil
}

// NewTriangulararbitrageFilterer creates a new log filterer instance of Triangulararbitrage, bound to a specific deployed contract.
func NewTriangulararbitrageFilterer(address common.Address, filterer bind.ContractFilterer) (*TriangulararbitrageFilterer, error) {
	contract, err := bindTriangulararbitrage(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &TriangulararbitrageFilterer{contract: contract}, nil
}

// bindTriangulararbitrage binds a generic wrapper to an already deployed contract.
func bindTriangulararbitrage(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := TriangulararbitrageMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Triangulararbitrage *TriangulararbitrageRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Triangulararbitrage.Contract.TriangulararbitrageCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Triangulararbitrage *TriangulararbitrageRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.TriangulararbitrageTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Triangulararbitrage *TriangulararbitrageRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.TriangulararbitrageTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Triangulararbitrage *TriangulararbitrageCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Triangulararbitrage.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Triangulararbitrage *TriangulararbitrageTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Triangulararbitrage *TriangulararbitrageTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.contract.Transact(opts, method, params...)
}

// ArbitrageQuery is a free data retrieval call binding the contract method 0x27e371f2.
//
// Solidity: function arbitrageQuery((address,address,address,address,address,address,address,address,address) t, uint256 startRatio, uint256 endRatio, uint256 pieces) view returns(int256[] roi)
func (_Triangulararbitrage *TriangulararbitrageCaller) ArbitrageQuery(opts *bind.CallOpts, t ITriangularArbitrageTriangular, startRatio *big.Int, endRatio *big.Int, pieces *big.Int) ([]*big.Int, error) {
	var out []interface{}
	err := _Triangulararbitrage.contract.Call(opts, &out, "arbitrageQuery", t, startRatio, endRatio, pieces)

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

func (_Triangulararbitrage *TriangulararbitrageCaller) GetData(t ITriangularArbitrageTriangular, startRatio *big.Int, endRatio *big.Int, pieces *big.Int) ([]byte, error) {
	data, err := _Triangulararbitrage.contract.GetData("arbitrageQuery", t, startRatio, endRatio, pieces)
	if err != nil {
		return nil, err
	}
	return data, err
}

// ArbitrageQuery is a free data retrieval call binding the contract method 0x27e371f2.
//
// Solidity: function arbitrageQuery((address,address,address,address,address,address,address,address,address) t, uint256 startRatio, uint256 endRatio, uint256 pieces) view returns(int256[] roi)
func (_Triangulararbitrage *TriangulararbitrageSession) ArbitrageQuery(t ITriangularArbitrageTriangular, startRatio *big.Int, endRatio *big.Int, pieces *big.Int) ([]*big.Int, error) {
	return _Triangulararbitrage.Contract.ArbitrageQuery(&_Triangulararbitrage.CallOpts, t, startRatio, endRatio, pieces)
}

// ArbitrageQuery is a free data retrieval call binding the contract method 0x27e371f2.
//
// Solidity: function arbitrageQuery((address,address,address,address,address,address,address,address,address) t, uint256 startRatio, uint256 endRatio, uint256 pieces) view returns(int256[] roi)
func (_Triangulararbitrage *TriangulararbitrageCallerSession) ArbitrageQuery(t ITriangularArbitrageTriangular, startRatio *big.Int, endRatio *big.Int, pieces *big.Int) ([]*big.Int, error) {
	return _Triangulararbitrage.Contract.ArbitrageQuery(&_Triangulararbitrage.CallOpts, t, startRatio, endRatio, pieces)
}

// IsTriangularValid is a free data retrieval call binding the contract method 0x79e0b71e.
//
// Solidity: function isTriangularValid((address,address,address,address,address,address,address,address,address) t, uint256 threshold) view returns(bool)
func (_Triangulararbitrage *TriangulararbitrageCaller) IsTriangularValid(opts *bind.CallOpts, t ITriangularArbitrageTriangular, threshold *big.Int) (bool, error) {
	var out []interface{}
	err := _Triangulararbitrage.contract.Call(opts, &out, "isTriangularValid", t, threshold)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsTriangularValid is a free data retrieval call binding the contract method 0x79e0b71e.
//
// Solidity: function isTriangularValid((address,address,address,address,address,address,address,address,address) t, uint256 threshold) view returns(bool)
func (_Triangulararbitrage *TriangulararbitrageSession) IsTriangularValid(t ITriangularArbitrageTriangular, threshold *big.Int) (bool, error) {
	return _Triangulararbitrage.Contract.IsTriangularValid(&_Triangulararbitrage.CallOpts, t, threshold)
}

// IsTriangularValid is a free data retrieval call binding the contract method 0x79e0b71e.
//
// Solidity: function isTriangularValid((address,address,address,address,address,address,address,address,address) t, uint256 threshold) view returns(bool)
func (_Triangulararbitrage *TriangulararbitrageCallerSession) IsTriangularValid(t ITriangularArbitrageTriangular, threshold *big.Int) (bool, error) {
	return _Triangulararbitrage.Contract.IsTriangularValid(&_Triangulararbitrage.CallOpts, t, threshold)
}

// ArbWcnwzblucpyf is a paid mutator transaction binding the contract method 0x00000000.
//
// Solidity: function arb_wcnwzblucpyf() returns()
func (_Triangulararbitrage *TriangulararbitrageTransactor) ArbWcnwzblucpyf(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Triangulararbitrage.contract.Transact(opts, "arb_wcnwzblucpyf")
}

// ArbWcnwzblucpyf is a paid mutator transaction binding the contract method 0x00000000.
//
// Solidity: function arb_wcnwzblucpyf() returns()
func (_Triangulararbitrage *TriangulararbitrageSession) ArbWcnwzblucpyf() (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.ArbWcnwzblucpyf(&_Triangulararbitrage.TransactOpts)
}

// ArbWcnwzblucpyf is a paid mutator transaction binding the contract method 0x00000000.
//
// Solidity: function arb_wcnwzblucpyf() returns()
func (_Triangulararbitrage *TriangulararbitrageTransactorSession) ArbWcnwzblucpyf() (*types.Transaction, error) {
	return _Triangulararbitrage.Contract.ArbWcnwzblucpyf(&_Triangulararbitrage.TransactOpts)
}
