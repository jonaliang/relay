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

package market

import (
	"errors"
	"fmt"
	"github.com/Loopring/relay/dao"
	"github.com/Loopring/relay/eventemiter"
	"github.com/Loopring/relay/market/util"
	"github.com/Loopring/relay/types"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	OneHour = "1Hr"
	//TwoHour = "2Hr"
	//OneDay = "1Day"
)

type Ticker struct {
	Market    string  `json:"market"`
	Intervals string  `json:"interval"`
	Amount    float64 `json:"amount"`
	Vol       float64 `json:"vol"`
	Open      float64 `json:"open"`
	Close     float64 `json:"close"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Last      float64 `json:"last"`
	Buy       string  `json:"buy"`
	Sell      string  `json:"sell"`
	Change    string  `json:"change"`
}

type Cache struct {
	Trends []Trend
	Fills  []dao.FillEvent
}

type Trend struct {
	Intervals  string
	Market     string
	Vol        float64
	Amount     float64
	CreateTime int64
	Open       float64
	Close      float64
	High       float64
	Low        float64
	Start      int64
	End        int64
}

type TrendManager struct {
	c          *cache.Cache
	cacheReady bool
	rds        dao.RdsService
	cron       *cron.Cron
}

var once sync.Once
var trendManager TrendManager

const trendKey = "market_ticker"
const tickerKey = "market_ticker_view"

func NewTrendManager(dao dao.RdsService) TrendManager {

	once.Do(func() {
		trendManager = TrendManager{rds: dao, cron: cron.New()}
		trendManager.c = cache.New(cache.NoExpiration, cache.NoExpiration)
		trendManager.refreshCache()
		trendManager.startScheduleUpdate()
		fillOrderWatcher := &eventemitter.Watcher{Concurrent: false, Handle: trendManager.handleOrderFilled}
		eventemitter.On(eventemitter.OrderManagerExtractorFill, fillOrderWatcher)
		//trendManager.startScheduleUpdate()
	})

	return trendManager
}

// ======> init cache steps
// step.1 init all market
// step.2 get all trend record into cache
// step.3 get all order fillFilled into cache
// step.4 calculate 24hr ticker
// step.5 send channel cache ready
// step.6 start schedule update

func (t *TrendManager) refreshCache() {

	log.Println("start refresh cache......")

	trendMap := make(map[string]Cache)
	tickerMap := make(map[string]Ticker)
	for _, mkt := range util.AllMarkets {
		mktCache := Cache{}
		mktCache.Trends = make([]Trend, 0)
		mktCache.Fills = make([]dao.FillEvent, 0)

		// default 100 records load first time
		trends, err := t.rds.TrendPageQuery(dao.Trend{Market: mkt}, 1, 100)

		if err != nil {
			log.Println(err)
			return
		}

		for _, trend := range trends.Data {
			mktCache.Trends = append(mktCache.Trends, ConvertUp(trend.(dao.Trend)))
		}

		now := time.Now()
		firstSecondThisHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 1, 0, now.Location())
		fills, err := t.rds.QueryRecentFills(mkt, "", firstSecondThisHour.Unix(), 0)
		if err != nil {
			log.Println(err)
			return
		}

		for _, f := range fills {
			mktCache.Fills = append(mktCache.Fills, f)
		}

		trendMap[mkt] = mktCache

		ticker := calculateTicker(mkt, fills, mktCache.Trends, firstSecondThisHour)
		tickerMap[mkt] = ticker
	}
	t.c.Set(trendKey, trendMap, cache.NoExpiration)
	t.c.Set(tickerKey, tickerMap, cache.NoExpiration)

	t.cacheReady = true

}

func calculateTicker(market string, fills []dao.FillEvent, trends []Trend, now time.Time) Ticker {

	var result = Ticker{Market: market}

	if len(fills) == 0 && len(trends) == 0 {
		return result
	}

	before24Hour := now.Unix() - 24*60*60

	var (
		high   float64
		low    float64
		vol    float64
		amount float64
	)

	sort.Slice(trends, func(i, j int) bool {
		return trends[i].Start < trends[j].Start
	})

	for _, data := range trends {

		if data.Start < before24Hour {
			continue
		}

		vol += data.Vol
		amount += data.Amount

		if result.Open == 0 && data.Open != 0 {
			result.Open = data.Open
		}

		if high == 0 || high < data.High {
			high = data.High
		}
		if low == 0 || (low > data.Low && data.Low != 0) {
			low = data.Low
		}

		if data.Close != 0 {
			result.Last = data.Close
			result.Close = data.Close
		}
	}

	for _, data := range fills {

		if util.IsBuy(data.TokenS) {
			vol += util.StringToFloat(data.AmountB)
			amount += util.StringToFloat(data.AmountS)
		} else {
			vol += util.StringToFloat(data.AmountS)
			amount += util.StringToFloat(data.AmountB)
		}

		price := util.CalculatePrice(data.AmountS, data.AmountB, data.TokenS, data.TokenB)

		if result.Open == 0 && price != 0 {
			result.Open = price
		}

		if price != 0 {
			result.Last = price
			result.Close = price
		}

		if high == 0 || high < price {
			high = price
		}
		if low == 0 || (low > price && price != 0) {
			low = price
		}
	}

	result.High = high
	result.Low = low

	if result.Open > 0 && result.Last > 0 {
		result.Change = fmt.Sprintf("%.2f%%", 100*(result.Last-result.Open)/result.Open)
	}

	result.Vol = vol
	result.Amount = amount
	return result
}

func (t *TrendManager) startScheduleUpdate() {
	t.cron.AddFunc("10 0 * * * *", t.insertTrend)
	t.cron.Start()
}

func (t *TrendManager) insertTrend() {
	// get latest 24 hour trend if not exist generate

	fmt.Println("start insert trend cron job")

	var wg sync.WaitGroup

	for _, mkt := range util.AllMarkets {
		now := time.Now()
		firstSecondThisHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 1, 0, now.Location())

		wg.Add(1)
		go func(tmpMkt string) {
			for i := 24; i >= 1; i-- {

				start := firstSecondThisHour.Unix() - int64(i*60*60)
				end := firstSecondThisHour.Unix() - int64((i-1)*60*60) - 1

				trends, _ := t.rds.TrendQueryByTime(OneHour, tmpMkt, start, end)
				if len(trends) > 0 {
					log.Println("current interval trend exsit")
					wg.Done()
					return
				}

				lastTrends, _ := t.rds.TrendQueryByTime(OneHour, tmpMkt, start-int64(60*60), end-int64(60*60))
				if len(lastTrends) > 1 {
					log.Println("found more than one last trend!")
					wg.Done()
					return
				} else if len(lastTrends) == 0 {
					log.Println("not found last trend!")
				}

				if trends == nil || len(trends) == 0 {
					fills, fillsErr := t.rds.QueryRecentFills(tmpMkt, "", start, end)

					if fillsErr != nil {
						continue
					}

					toInsert := &dao.Trend{
						Intervals:  OneHour,
						Market:     tmpMkt,
						CreateTime: time.Now().Unix(),
						Start:      start,
						End:        end}

					var (
						vol    float64
						amount float64
						open   float64
						low    float64
					)

					if len(lastTrends) == 0 {
						toInsert.Open = 0
						toInsert.Close = 0
						toInsert.High = 0
						toInsert.Low = 0
					} else {
						toInsert.Open = lastTrends[0].Close
						toInsert.Close = lastTrends[0].Close
						toInsert.High = lastTrends[0].Close
						toInsert.Low = lastTrends[0].Close
					}

					sort.Slice(fills, func(i, j int) bool {
						return fills[i].CreateTime < fills[j].CreateTime
					})

					for _, data := range fills {

						if util.IsBuy(data.TokenS) {
							vol += util.StringToFloat(data.AmountB)
							amount += util.StringToFloat(data.AmountS)
						} else {
							vol += util.StringToFloat(data.AmountS)
							amount += util.StringToFloat(data.AmountB)
						}

						price := util.CalculatePrice(data.AmountS, data.AmountB, data.TokenS, data.TokenB)

						if open == 0 && price != 0 {
							open = price
						}

						if toInsert.High == 0 || toInsert.High < price {
							toInsert.High = price
						}
						if low == 0 || low > price {
							low = price
						}
						toInsert.Close = price
					}

					if toInsert.Open != 0 && open != 0 {
						toInsert.Open = open
					}

					toInsert.Low = low
					toInsert.Vol = vol
					toInsert.Amount = amount

					if err := t.rds.Add(toInsert); err != nil {
						fmt.Println(err)
					}
				}
			}
			wg.Done()
		}(mkt)
	}
	wg.Wait()
	t.refreshCache()
}

func (t *TrendManager) aggregate(fills []dao.FillEvent, trends []Trend) (trend Trend, err error) {

	if len(fills) == 0 && len(trends) == 0 {
		err = errors.New("fills can't be nil")
		return
	}

	now := time.Now()
	firstSecondThisHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 1, 0, now.Location())
	lastSecondThisHour := firstSecondThisHour.Unix() + 60*60 - 1

	if len(fills) == 0 {
		lastTrend := trends[len(trends)-1]
		return Trend{
			Intervals:  OneHour,
			Market:     lastTrend.Market,
			CreateTime: time.Now().Unix(),
			Start:      firstSecondThisHour.Unix(),
			End:        lastSecondThisHour,
			High:       lastTrend.High,
			Low:        lastTrend.Low,
			Vol:        0,
			Amount:     0,
			Open:       lastTrend.Open,
			Close:      lastTrend.Close,
		}, nil
	}

	trend = Trend{
		Intervals:  OneHour,
		Market:     fills[0].Market,
		CreateTime: time.Now().Unix(),
		Start:      firstSecondThisHour.Unix(),
		End:        lastSecondThisHour,
	}

	var (
		high   float64
		low    float64
		vol    float64
		amount float64
	)

	sort.Slice(fills, func(i, j int) bool {
		return fills[i].CreateTime < fills[j].CreateTime
	})

	for _, data := range fills {

		if util.IsBuy(data.TokenS) {
			vol += util.StringToFloat(data.AmountB)
			amount += util.StringToFloat(data.AmountS)
		} else {
			vol += util.StringToFloat(data.AmountS)
			amount += util.StringToFloat(data.AmountB)
		}

		price := util.CalculatePrice(data.AmountS, data.AmountB, data.TokenS, data.TokenB)

		if high == 0 || high < price {
			high = price
		}
		if low == 0 || low > price {
			low = price
		}
	}

	trend.High = high
	trend.Low = low

	openFill := fills[0]
	trend.Open = util.CalculatePrice(openFill.AmountS, openFill.AmountB, openFill.TokenS, openFill.TokenB)
	closeFill := fills[len(fills)-1]
	trend.Close = util.CalculatePrice(closeFill.AmountS, closeFill.AmountB, closeFill.TokenS, closeFill.TokenB)

	trend.Vol = vol
	trend.Amount = amount

	return
}

func (t *TrendManager) GetTrends(market string) (trends []Trend, err error) {

	market = strings.ToUpper(market)

	if t.cacheReady {
		if trendCache, ok := t.c.Get(trendKey); !ok {
			err = errors.New("can't found trends by key : " + trendKey)
		} else {
			tc := trendCache.(map[string]Cache)[market]
			trends = make([]Trend, 0)
			trendInFills, aggErr := t.aggregate(tc.Fills, tc.Trends)
			if aggErr == nil {
				trends = append(trends, trendInFills)
			}
			for _, t := range tc.Trends {
				trends = append(trends, t)
			}

		}
	} else {
		err = errors.New("cache is not ready , please access later")
	}
	return
}

func (t *TrendManager) GetTicker() (tickers []Ticker, err error) {

	if t.cacheReady {
		if tickerInCache, ok := t.c.Get(tickerKey); ok {
			tickerMap := tickerInCache.(map[string]Ticker)
			tickers = make([]Ticker, 0)
			for _, v := range tickerMap {
				v.Buy = strconv.FormatFloat(v.Last, 'f', -1, 64)
				v.Sell = strconv.FormatFloat(v.Last, 'f', -1, 64)
				tickers = append(tickers, v)
			}
		} else {
			err = errors.New("get ticker from cache error, no value found")
		}
	} else {
		err = errors.New("cache is not ready , please access later")
	}
	return
}

func (t *TrendManager) handleOrderFilled(input eventemitter.EventData) (err error) {

	if t.cacheReady {

		event := input.(*types.OrderFilledEvent)
		newFillModel := &dao.FillEvent{}
		if err = newFillModel.ConvertDown(event); err != nil {
			return
		}

		market, wrapErr := util.WrapMarketByAddress(newFillModel.TokenS, newFillModel.TokenB)

		if wrapErr != nil {
			err = wrapErr
			return
		}

		if tickerInCache, ok := t.c.Get(trendKey); ok {
			trendMap := tickerInCache.(map[string]Cache)
			tc := trendMap[market]
			tc.Fills = append(tc.Fills, *newFillModel)
			trendMap[market] = tc
			t.c.Set(trendKey, trendMap, cache.NoExpiration)
			t.reCalTicker(market)
		} else {
			fills := make([]dao.FillEvent, 0)
			fills = append(fills, *newFillModel)
			newCache := Cache{make([]Trend, 0), fills}
			t.c.Set(trendKey, newCache, cache.NoExpiration)
			t.reCalTicker(market)
		}
	} else {
		err = errors.New("cache is not ready , please access later")
	}

	return
}

func (t *TrendManager) reCalTicker(market string) {
	trendInCache, _ := t.c.Get(trendKey)
	mktCache := trendInCache.(map[string]Cache)[market]
	now := time.Now()
	firstSecondThisHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 1, 0, now.Location())
	ticker := calculateTicker(market, mktCache.Fills, mktCache.Trends, firstSecondThisHour)
	tickerInCache, _ := t.c.Get(tickerKey)
	tickerMap := tickerInCache.(map[string]Ticker)
	tickerMap[market] = ticker
}

func ConvertUp(src dao.Trend) Trend {

	return Trend{
		Intervals:  src.Intervals,
		Market:     src.Market,
		Vol:        src.Vol,
		Amount:     src.Amount,
		CreateTime: src.CreateTime,
		Open:       src.Open,
		Close:      src.Close,
		High:       src.High,
		Low:        src.Low,
		Start:      src.Start,
		End:        src.End,
	}
}
