/*

  Copyright 2017 Loopring Project Ltd (Loopring Foundation).

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

*/

package extractor

import (
	"encoding/json"
	"fmt"
	"github.com/Loopring/relay/config"
	"github.com/Loopring/relay/dao"
	"github.com/Loopring/relay/ethaccessor"
	"github.com/Loopring/relay/eventemiter"
	"github.com/Loopring/relay/log"
	"github.com/Loopring/relay/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"math/big"
	"sync"
)

/**
区块链的listener, 得到order以及ring的事件，
*/

type ExtractorService interface {
	Start()
	Stop()
	Fork(start *big.Int)
}

// TODO(fukun):不同的channel，应当交给orderbook统一进行后续处理，可以将channel作为函数返回值、全局变量、参数等方式
type ExtractorServiceImpl struct {
	options          config.AccessorOptions
	commOpts         config.CommonOptions
	accessor         *ethaccessor.EthNodeAccessor
	detector         *forkDetector
	processor        *AbiProcessor
	dao              dao.RdsService
	stop             chan bool
	lock             sync.RWMutex
	startBlockNumber *big.Int
	endBlockNumber   *big.Int
	iterator         *ethaccessor.BlockIterator
	syncComplete     bool
	forkComplete     bool
	forktest         bool
}

func NewExtractorService(commonOpts config.CommonOptions,
	accessor *ethaccessor.EthNodeAccessor,
	rds dao.RdsService) *ExtractorServiceImpl {
	var l ExtractorServiceImpl

	l.commOpts = commonOpts
	l.accessor = accessor
	l.dao = rds
	l.processor = newAbiProcessor(accessor, rds)
	l.detector = newForkDetector(rds, accessor)
	l.stop = make(chan bool, 1)

	start, end := l.getBlockNumberRange()
	l.setBlockNumberRange(start, end)

	return &l
}

func (l *ExtractorServiceImpl) Start() {
	log.Info("extractor start...")
	l.syncComplete = false

	l.iterator = l.accessor.BlockIterator(l.startBlockNumber, l.endBlockNumber, false, uint64(0))
	go func() {
		for {
			select {
			case <-l.stop:
				return
			default:
				l.processBlock()
			}
		}
	}()
}

func (l *ExtractorServiceImpl) Stop() {
	l.stop <- true
}

// 重启(分叉)时先关停subscribeEvents，然后关
func (l *ExtractorServiceImpl) Fork(start *big.Int) {
	l.setBlockNumberRange(start, nil)
}

func (l *ExtractorServiceImpl) sync(blockNumber *big.Int) {
	var syncBlock types.Big
	if err := l.accessor.RetryCall(2, &syncBlock, "eth_blockNumber"); err != nil {
		log.Fatalf("extractor,sync chain block,get ethereum node current block number error:%s", err.Error())
	}
	if syncBlock.BigInt().Cmp(blockNumber) <= 0 {
		eventemitter.Emit(eventemitter.SyncChainComplete, syncBlock)
		l.syncComplete = true
		l.debug("extractor,sync chain block complete!")
	} else {
		l.debug("extractor,chain block syncing... ")
	}
}

func (l *ExtractorServiceImpl) processBlock() {
	inter, err := l.iterator.Next()
	if err != nil {
		log.Fatalf("extractor,iterator next error:%s", err.Error())
	}

	// get current block
	block := inter.(*ethaccessor.BlockWithTxHash)
	log.Infof("extractor,get block:%s->%s, transactions:%d", block.Number.BigInt().String(), block.Hash.Hex(), len(block.Transactions))

	currentBlock := &types.Block{}
	currentBlock.BlockNumber = block.Number.BigInt()
	currentBlock.ParentHash = block.ParentHash
	currentBlock.BlockHash = block.Hash
	currentBlock.CreateTime = block.Timestamp.Int64()

	// sync blocks on chain
	if l.syncComplete == false {
		l.sync(currentBlock.BlockNumber)
	}

	// detect chain fork
	// todo free fork detector
	// l.detector.Detect(currentBlock)

	// convert block to dao entity
	var entity dao.Block
	if err := entity.ConvertDown(currentBlock); err != nil {
		l.debug("extractor,convert block to dao/entity error:%s", err.Error())
	} else {
		l.dao.Add(&entity)
	}

	// emit new block
	blockEvent := &types.BlockEvent{}
	blockEvent.BlockNumber = block.Number.BigInt()
	blockEvent.BlockHash = block.Hash
	eventemitter.Emit(eventemitter.Block_New, blockEvent)

	// process block
	for _, tx := range block.Transactions {
		logAmount, err := l.processEvent(tx, block.Timestamp.BigInt())
		if err != nil {
			log.Errorf(err.Error())
		}

		// 解析method，获得ring内等orders并发送到orderbook保存
		if err := l.processMethod(tx, block.Timestamp.BigInt(), block.Number.BigInt(), logAmount); err != nil {
			log.Errorf(err.Error())
		}
	}
}

func (l *ExtractorServiceImpl) processMethod(txhash string, time, blockNumber *big.Int, logAmount int) error {
	var tx ethaccessor.Transaction
	if err := l.accessor.RetryCall(2, &tx, "eth_getTransactionByHash", txhash); err != nil {
		return fmt.Errorf("extractor,get transaction error:%s", err.Error())
	}

	if !l.processor.HasContract(common.HexToAddress(tx.To)) {
		l.debug("extractor,unspported protocol %s", tx.To)
		return nil
	}

	input := common.FromHex(tx.Input)
	var (
		method MethodData
		ok     bool
	)

	// 过滤方法
	if len(input) < 4 || len(tx.Input) < 10 {
		l.debug("extractor,contract method id %s length invalid", common.ToHex(input))
		return nil
	}

	id := common.ToHex(input[0:4])
	if method, ok = l.processor.GetMethod(id); !ok {
		l.debug("extractor,contract method id error:%s", id)
		return nil
	}

	method.FullFilled(&tx, time, logAmount)

	eventemitter.Emit(method.Id, method)
	return nil
}

func (l *ExtractorServiceImpl) processEvent(txhash string, time *big.Int) (int, error) {
	var receipt ethaccessor.TransactionReceipt

	if err := l.accessor.RetryCall(5, &receipt, "eth_getTransactionReceipt", txhash); err != nil {
		return 0, fmt.Errorf("extractor,get transaction %s receipt error:%s", txhash, err.Error())
	}

	if !l.processor.HasContract(common.HexToAddress(receipt.To)) {
		l.debug("extractor,unspported protocol %s", receipt.To)
		return 0, nil
	}

	if len(receipt.Logs) == 0 {
		l.debug("extractor,transaction %s recipient do not have any logs", txhash)
		return 0, nil
	}

	for _, evtLog := range receipt.Logs {
		var (
			event EventData
			ok    bool
		)

		// 过滤合约
		protocolAddr := common.HexToAddress(evtLog.Address)
		if ok := l.processor.HasContract(protocolAddr); !ok {
			l.debug("extractor, unsupported protocol %s", protocolAddr.Hex())
			continue
		}

		// 过滤事件
		data := hexutil.MustDecode(evtLog.Data)
		id := common.HexToHash(evtLog.Topics[0])
		if event, ok = l.processor.GetEvent(id); !ok {
			l.debug("extractor,contract event id error:%s", id.Hex())
			continue
		}

		// 记录event log
		if l.commOpts.SaveEventLog {
			if bs, err := json.Marshal(evtLog); err != nil {
				l.debug("extractor,json unmarshal evtlog error:%s", err.Error())
			} else {
				el := &dao.EventLog{}
				el.Protocol = evtLog.Address
				el.TxHash = txhash
				el.BlockNumber = evtLog.BlockNumber.Int64()
				el.CreateTime = time.Int64()
				el.Data = bs
				l.dao.Add(el)
			}
		}

		if nil != data && len(data) > 0 {
			// 解析事件
			if err := event.CAbi.Unpack(event.Event, event.Name, data, abi.SEL_UNPACK_EVENT); nil != err {
				log.Errorf("extractor,unpack event error:%s", err.Error())
				continue
			}
		}

		// full filled event and emit to abi processor
		event.FullFilled(&evtLog, time, txhash)
		eventemitter.Emit(event.Id.Hex(), event)
	}

	return len(receipt.Logs), nil
}

func (l *ExtractorServiceImpl) setBlockNumberRange(start, end *big.Int) {
	l.startBlockNumber = start
	if end != nil {
		l.endBlockNumber = end
	}
}

func (l *ExtractorServiceImpl) getBlockNumberRange() (*big.Int, *big.Int) {
	var ret types.Block

	start := l.commOpts.DefaultBlockNumber
	end := l.commOpts.EndBlockNumber

	// 寻找分叉块，并归零分叉标记
	forkBlock, err := l.dao.FindForkBlock()
	if err == nil {
		blockHash := common.HexToHash(forkBlock.BlockHash)
		l.dao.SetForkBlock(blockHash)
		return ret.BlockNumber, end
	}

	// 寻找最新块
	latestBlock, err := l.dao.FindLatestBlock()
	if err != nil {
		l.debug("extractor,get latest block number error:%s", err.Error())
		return start, end
	}
	if err := latestBlock.ConvertUp(&ret); err != nil {
		log.Fatalf("extractor,get blocknumber range convert up error:%s", err.Error())
	}

	return ret.BlockNumber, end
}

func (l *ExtractorServiceImpl) debug(template string, args ...interface{}) {
	if l.commOpts.Develop {
		log.Debugf(template, args...)
	}
}
