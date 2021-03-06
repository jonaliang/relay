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

package extractor_test

import (
	"github.com/Loopring/relay/ethaccessor"
	"github.com/Loopring/relay/test"
	"github.com/ethereum/bak/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"testing"
)

func TestExtractorServiceImpl_UnpackSubmitRingMethod(t *testing.T) {
	input := "0xca35947d000000000000000000000000000000000000000000000000000000000000012000000000000000000000000000000000000000000000000000000000000001c000000000000000000000000000000000000000000000000000000000000003a0000000000000000000000000000000000000000000000000000000000000044000000000000000000000000000000000000000000000000000000000000004a0000000000000000000000000000000000000000000000000000000000000052000000000000000000000000000000000000000000000000000000000000005a00000000000000000000000004bad3053d574cd54513babe21db3f09bea1d387d0000000000000000000000004bad3053d574cd54513babe21db3f09bea1d387d0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000b1018949b241d76a1ab2094f473e9befeabb5ead000000000000000000000000fc2cbce778ddbc4d50bb5b2fc91afe14a8e3953d0000000000000000000000001b978a1d302335a6f2ebe4b8823b5e17c3c84135000000000000000000000000876c8b6ff4a8e87dc6d5e3f64715b58be7d5ab55000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000003e80000000000000000000000000000000000000000000000000000000000000064000000000000000000000000000000000000000000000000000000005a2f69d5000000000000000000000000000000000000000000000000000000000083d60000000000000000000000000000000000000000000000000000000000000003e800000000000000000000000000000000000000000000000001acd168ff1ede9900000000000000000000000000000000000000000000000000000000000003e8000000000000000000000000000000000000000000000000000000000000006400000000000000000000000000000000000000000000000000000000000003e8000000000000000000000000000000000000000000000000000000005a2f69d5000000000000000000000000000000000000000000000000000000000083d60000000000000000000000000000000000000000000000000000000000000003e800000000000000000000000000000000000000000000000001acd168ff1ede990000000000000000000000000000000000000000000000000000000000000064000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000000000000000001b000000000000000000000000000000000000000000000000000000000000001b000000000000000000000000000000000000000000000000000000000000001c000000000000000000000000000000000000000000000000000000000000000307347720467c2f9e24fd6f1a9c99a0f84845464de630201183c274bebc4b7318be37861175ea412728e1ad9306e356da6b1484b20d4f046990c055687c103508efcec603fa2aa5003ef6e65339b4ebd894ead93b5558ebfcb36333d014e295d400000000000000000000000000000000000000000000000000000000000000034c77ebd7557a24d7cba0d601af101db98a5a5b2ebe0ec4f0ea64a96996b9e7423befef8f626b65510501809f852aab99d520fd1e00e34e806bf5400f7875cb9f3b16042be38b3b7996ac5fd0ffff5c321e9110d6c91cd2bd5d90a0a87d5190c3"

	var ring ethaccessor.SubmitRingMethod
	accessor, _ := test.GenerateAccessor()

	data := hexutil.MustDecode("0x" + input[10:])

	if err := accessor.ProtocolImplAbi.UnpackMethodInput(&ring, "submitRing", data); err != nil {
		t.Fatalf(err.Error())
	}

	orders, err := ring.ConvertDown()
	if err != nil {
		t.Fatalf(err.Error())
	}

	for k, v := range orders {
		t.Log(k, "tokenS", v.TokenS.Hex())
		t.Log(k, "tokenB", v.TokenB.Hex())

		t.Log(k, "amountS", v.AmountS.String())
		t.Log(k, "amountB", v.AmountB.String())
		t.Log(k, "timestamp", v.Timestamp.String())
		t.Log(k, "ttl", v.Ttl.String())
		t.Log(k, "salt", v.Salt.String())
		t.Log(k, "lrcFee", v.LrcFee.String())
		t.Log(k, "rateAmountS", ring.UintArgsList[k][6].String())

		t.Log(k, "marginSplitpercentage", v.MarginSplitPercentage)
		t.Log(k, "feeSelectionList", ring.Uint8ArgsList[k][1])

		t.Log(k, "buyNoMoreThanAmountB", v.BuyNoMoreThanAmountB)

		t.Log("v", v.V)
		t.Log("s", v.S.Hex())
		t.Log("r", v.R.Hex())
	}

	t.Log("ringminer", ring.RingMiner.Hex())
	t.Log("feeRecipient", ring.FeeRecipient.Hex())
}

func TestExtractorServiceImpl_UnpackWethWithdrawalMethod(t *testing.T) {
	input := "0x2e1a7d4d0000000000000000000000000000000000000000000000000000000000000064"

	var withdrawal ethaccessor.WethWithdrawalMethod
	accessor, _ := test.GenerateAccessor()

	data := hexutil.MustDecode("0x" + input[10:])

	if err := accessor.WethAbi.UnpackMethodInput(&withdrawal.Value, "withdraw", data); err != nil {
		t.Fatalf(err.Error())
	}

	evt := withdrawal.ConvertDown()
	t.Logf("withdrawal event value:%s", evt.Value)
}

func TestExtractorServiceImpl_UnpackSubmitRingHashMethod(t *testing.T) {
	input := "0xae201a700000000000000000000000004bad3053d574cd54513babe21db3f09bea1d387da0b3871b768ec39f2d21e782177cca3762caafeb93a74b56e377b9cbc18e7c1f"

	var method ethaccessor.SubmitRingHashMethod
	accessor, _ := test.GenerateAccessor()

	data := hexutil.MustDecode("0x" + input[10:])

	if err := accessor.RinghashRegistryAbi.UnpackMethodInput(&method, "submitRinghash", data); err != nil {
		t.Fatalf(err.Error())
	}

	ringhash, err := method.ConvertDown()
	if err != nil {
		t.Fatalf(err.Error())
	}

	t.Logf("ringhash:%s, ringminer:%s", ringhash.RingHash.Hex(), ringhash.RingMiner.Hex())
}

func TestExtractorServiceImpl_UnpackCancelOrderMethod(t *testing.T) {
	input := "0x47a99e43000000000000000000000000b1018949b241d76a1ab2094f473e9befeabb5ead000000000000000000000000529540ee6862158f47d647ae023098f6705210a9000000000000000000000000667b8a1021c324b4f42e77d46f5a7a2a2a3cdfc60000000000000000000000000000000000000000000000000000000000004e2000000000000000000000000000000000000000000000000000000000000003e8000000000000000000000000000000000000000000000000000000005a33d324000000000000000000000000000000000000000000000000000000000083d60000000000000000000000000000000000000000000000000000000000000003e80000000000000000000000000000000000000000000000000000000000002710000000000000000000000000000000000000000000000000000000000000006400000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001c8c2ccb736eb22424dee71115565d46a1fcf91beb1b12a59488de2757254051020a9d5b0698742580b4a7ec2090e1f24b8b466be253e162994e4887720446807c"

	var method ethaccessor.CancelOrderMethod
	accessor, _ := test.GenerateAccessor()

	data := hexutil.MustDecode("0x" + input[10:])

	for i := 0; i < len(data)/32; i++ {
		t.Logf("index:%d -> %s", i, common.ToHex(data[i*32:(i+1)*32]))
	}

	if err := accessor.ProtocolImplAbi.UnpackMethodInput(&method, "cancelOrder", data); err != nil {
		t.Fatalf(err.Error())
	}

	order, err := method.ConvertDown()
	if err != nil {
		t.Fatalf(err.Error())
	}

	t.Log("owner", order.Owner.Hex())
	t.Log("tokenS", order.TokenS.Hex())
	t.Log("tokenB", order.TokenB.Hex())
	t.Log("amountS", order.AmountS.String())
	t.Log("amountB", order.AmountB.String())
	t.Log("timestamp", order.Timestamp.String())
	t.Log("ttl", order.Ttl.String())
	t.Log("salt", order.Salt.String())
	t.Log("lrcFee", order.LrcFee.String())
	t.Log("cancelAmount", method.OrderValues[6].String())
	t.Log("buyNoMoreThanAmountB", order.BuyNoMoreThanAmountB)
	t.Log("marginSplitpercentage", order.MarginSplitPercentage)
	t.Log("v", order.V)
	t.Log("s", order.S.Hex())
	t.Log("r", order.R.Hex())
}

func TestExtractorServiceImpl_UnpackApproveMethod(t *testing.T) {
	input := "0x095ea7b300000000000000000000000045aa504eb94077eec4bf95a10095a8e3196fc5910000000000000000000000000000000000000000000000008ac7230489e80000"

	var method ethaccessor.ApproveMethod
	accessor, _ := test.GenerateAccessor()

	data := hexutil.MustDecode("0x" + input[10:])
	for i := 0; i < len(data)/32; i++ {
		t.Logf("index:%d -> %s", i, common.ToHex(data[i*32:(i+1)*32]))
	}

	if err := accessor.Erc20Abi.UnpackMethodInput(&method, "approve", data); err != nil {
		t.Fatalf(err.Error())
	}

	approve := method.ConvertDown()
	t.Logf("approve spender:%s, value:%s", approve.Spender.Hex(), approve.Value.String())
}
