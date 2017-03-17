package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/thrasher-/gocryptotrader/common"
	"github.com/thrasher-/gocryptotrader/config"
	"github.com/thrasher-/gocryptotrader/exchanges"
)

const (
	GEMINI_API_URL     = "https://api.gemini.com"
	GEMINI_API_VERSION = "1"

	GEMINI_SYMBOLS              = "symbols"
	GEMINI_TICKER               = "pubticker"
	GEMINI_AUCTION              = "auction"
	GEMINI_AUCTION_HISTORY      = "history"
	GEMINI_ORDERBOOK            = "book"
	GEMINI_TRADES               = "trades"
	GEMINI_ORDERS               = "orders"
	GEMINI_ORDER_NEW            = "order/new"
	GEMINI_ORDER_CANCEL         = "order/cancel"
	GEMINI_ORDER_CANCEL_SESSION = "order/cancel/session"
	GEMINI_ORDER_CANCEL_ALL     = "order/cancel/all"
	GEMINI_ORDER_STATUS         = "order/status"
	GEMINI_MYTRADES             = "mytrades"
	GEMINI_BALANCES             = "balances"
	GEMINI_HEARTBEAT            = "heartbeat"
)

type Gemini struct {
	exchange.ExchangeBase
}

type GeminiOrderbookEntry struct {
	Price    float64 `json:"price,string"`
	Quantity float64 `json:"quantity,string"`
}

type GeminiOrderbook struct {
	Bids []GeminiOrderbookEntry `json:"bids"`
	Asks []GeminiOrderbookEntry `json:"asks"`
}

type GeminiTrade struct {
	Timestamp int64   `json:"timestamp"`
	TID       int64   `json:"tid"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
	Side      string  `json:"taker"`
}

type GeminiOrder struct {
	OrderID           int64   `json:"order_id"`
	ClientOrderID     string  `json:"client_order_id"`
	Symbol            string  `json:"symbol"`
	Exchange          string  `json:"exchange"`
	Price             float64 `json:"price,string"`
	AvgExecutionPrice float64 `json:"avg_execution_price,string"`
	Side              string  `json:"side"`
	Type              string  `json:"type"`
	Timestamp         int64   `json:"timestamp"`
	TimestampMS       int64   `json:"timestampms"`
	IsLive            bool    `json:"is_live"`
	IsCancelled       bool    `json:"is_cancelled"`
	WasForced         bool    `json:"was_forced"`
	ExecutedAmount    float64 `json:"executed_amount,string"`
	RemainingAmount   float64 `json:"remaining_amount,string"`
	OriginalAmount    float64 `json:"original_amount,string"`
}

type GeminiOrderResult struct {
	Result bool `json:"result"`
}

type GeminiTradeHistory struct {
	Price         float64 `json:"price"`
	Amount        float64 `json:"amount"`
	Timestamp     int64   `json:"timestamp"`
	TimestampMS   int64   `json:"timestampms"`
	Type          string  `json:"type"`
	FeeCurrency   string  `json:"fee_currency"`
	FeeAmount     float64 `json:"fee_amount"`
	TID           int64   `json:"tid"`
	OrderID       int64   `json:"order_id"`
	ClientOrderID string  `json:"client_order_id"`
}

type GeminiBalance struct {
	Currency  string  `json:"currency"`
	Amount    float64 `json:"amount"`
	Available float64 `json:"available"`
}

func (g *Gemini) SetDefaults() {
	g.Name = "Gemini"
	g.Enabled = false
	g.Verbose = false
	g.Websocket = false
	g.RESTPollingDelay = 10
}

func (g *Gemini) Setup(exch config.ExchangeConfig) {
	if !exch.Enabled {
		g.SetEnabled(false)
	} else {
		g.Enabled = true
		g.AuthenticatedAPISupport = exch.AuthenticatedAPISupport
		g.SetAPIKeys(exch.APIKey, exch.APISecret, "", false)
		g.RESTPollingDelay = exch.RESTPollingDelay
		g.Verbose = exch.Verbose
		g.Websocket = exch.Websocket
		g.BaseCurrencies = common.SplitStrings(exch.BaseCurrencies, ",")
		g.AvailablePairs = common.SplitStrings(exch.AvailablePairs, ",")
		g.EnabledPairs = common.SplitStrings(exch.EnabledPairs, ",")
	}
}

func (g *Gemini) Start() {
	go g.Run()
}

func (g *Gemini) Run() {
	if g.Verbose {
		log.Printf("%s polling delay: %ds.\n", g.GetName(), g.RESTPollingDelay)
		log.Printf("%s %d currencies enabled: %s.\n", g.GetName(), len(g.EnabledPairs), g.EnabledPairs)
	}

	exchangeProducts, err := g.GetSymbols()
	if err != nil {
		log.Printf("%s Failed to get available symbols.\n", g.GetName())
	} else {
		exchangeProducts = common.SplitStrings(common.StringToUpper(common.JoinStrings(exchangeProducts, ",")), ",")
		diff := common.StringSliceDifference(g.AvailablePairs, exchangeProducts)
		if len(diff) > 0 {
			exch, err := bot.config.GetExchangeConfig(g.Name)
			if err != nil {
				log.Println(err)
			} else {
				log.Printf("%s Updating available pairs. Difference: %s.\n", g.Name, diff)
				exch.AvailablePairs = common.JoinStrings(exchangeProducts, ",")
				bot.config.UpdateExchangeConfig(exch)
			}
		}
	}

	for g.Enabled {
		for _, x := range g.EnabledPairs {
			currency := x
			go func() {
				ticker, err := g.GetTickerPrice(currency)
				if err != nil {
					log.Println(err)
					return
				}
				log.Printf("Gemini %s Last %f Bid %f Ask %f Volume %f\n", currency, ticker.Last, ticker.Bid, ticker.Ask, ticker.Volume)
				AddExchangeInfo(g.GetName(), currency[0:3], currency[3:], ticker.Last, ticker.Volume)
			}()
		}
		time.Sleep(time.Second * g.RESTPollingDelay)
	}
}

type GeminiTicker struct {
	Ask    float64 `json:"ask,string"`
	Bid    float64 `json:"bid,string"`
	Last   float64 `json:"last,string"`
	Volume struct {
		Currency  float64
		USD       float64
		Timestamp int64
	}
}

func (g *Gemini) GetTicker(currency string) (GeminiTicker, error) {

	type TickerResponse struct {
		Ask    float64 `json:"ask,string"`
		Bid    float64 `json:"bid,string"`
		Last   float64 `json:"last,string"`
		Volume map[string]interface{}
	}

	ticker := GeminiTicker{}
	resp := TickerResponse{}
	path := fmt.Sprintf("%s/v%s/%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_TICKER, currency)

	err := common.SendHTTPGetRequest(path, true, &resp)
	if err != nil {
		return ticker, err
	}

	ticker.Ask = resp.Ask
	ticker.Bid = resp.Bid
	ticker.Last = resp.Last

	ticker.Volume.Currency, _ = strconv.ParseFloat(resp.Volume[currency[0:3]].(string), 64)
	ticker.Volume.USD, _ = strconv.ParseFloat(resp.Volume["USD"].(string), 64)

	time, _ := resp.Volume["timestamp"].(float64)
	ticker.Volume.Timestamp = int64(time)

	return ticker, nil
}

func (g *Gemini) GetTickerPrice(currency string) (TickerPrice, error) {
	tickerNew, err := GetTicker(g.GetName(), currency[0:3], currency[3:])
	if err == nil {
		return tickerNew, nil
	}

	var tickerPrice TickerPrice
	ticker, err := g.GetTicker(currency)
	if err != nil {
		return tickerPrice, err
	}
	tickerPrice.Ask = ticker.Ask
	tickerPrice.Bid = ticker.Bid
	tickerPrice.FirstCurrency = currency[0:3]
	tickerPrice.SecondCurrency = currency[3:]
	tickerPrice.CurrencyPair = tickerPrice.FirstCurrency + "_" + tickerPrice.SecondCurrency
	tickerPrice.Last = ticker.Last
	tickerPrice.Volume = ticker.Volume.USD
	ProcessTicker(g.GetName(), tickerPrice.FirstCurrency, tickerPrice.SecondCurrency, tickerPrice)
	return tickerPrice, nil
}

func (g *Gemini) GetSymbols() ([]string, error) {
	symbols := []string{}
	path := fmt.Sprintf("%s/v%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_SYMBOLS)
	err := common.SendHTTPGetRequest(path, true, &symbols)
	if err != nil {
		return nil, err
	}
	return symbols, nil
}

type GeminiAuction struct {
	LastAuctionPrice    float64 `json:"last_auction_price,string"`
	LastAuctionQuantity float64 `json:"last_auction_quantity,string"`
	LastHighestBidPrice float64 `json:"last_highest_bid_price,string"`
	LastLowestAskPrice  float64 `json:"last_lowest_ask_price,string"`
	NextUpdateMS        int64   `json:"next_update_ms"`
	NextAuctionMS       int64   `json:"next_auction_ms"`
	LastAuctionEID      int64   `json:"last_auction_eid"`
}

func (g *Gemini) GetAuction(currency string) (GeminiAuction, error) {
	path := fmt.Sprintf("%s/v%s/%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_AUCTION, currency)
	auction := GeminiAuction{}
	err := common.SendHTTPGetRequest(path, true, &auction)
	if err != nil {
		return auction, err
	}
	return auction, nil
}

type GeminiAuctionHistory struct {
	AuctionID       int64   `json:"auction_id"`
	AuctionPrice    float64 `json:"auction_price,string"`
	AuctionQuantity float64 `json:"auction_quantity,string"`
	EID             int64   `json:"eid"`
	HighestBidPrice float64 `json:"highest_bid_price,string"`
	LowestAskPrice  float64 `json:"lowest_ask_price,string"`
	AuctionResult   string  `json:"auction_result"`
	Timestamp       int64   `json:"timestamp"`
	TimestampMS     int64   `json:"timestampms"`
	EventType       string  `json:"event_type"`
}

func (g *Gemini) GetAuctionHistory(currency string, params url.Values) ([]GeminiAuctionHistory, error) {
	path := common.EncodeURLValues(fmt.Sprintf("%s/v%s/%s/%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_AUCTION, currency, GEMINI_AUCTION_HISTORY), params)
	auctionHist := []GeminiAuctionHistory{}
	err := common.SendHTTPGetRequest(path, true, &auctionHist)
	if err != nil {
		return nil, err
	}
	return auctionHist, nil
}

func (g *Gemini) GetOrderbook(currency string, params url.Values) (GeminiOrderbook, error) {
	path := common.EncodeURLValues(fmt.Sprintf("%s/v%s/%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_ORDERBOOK, currency), params)
	orderbook := GeminiOrderbook{}
	err := common.SendHTTPGetRequest(path, true, &orderbook)
	if err != nil {
		return GeminiOrderbook{}, err
	}

	return orderbook, nil
}

func (g *Gemini) GetTrades(currency string, params url.Values) ([]GeminiTrade, error) {
	path := common.EncodeURLValues(fmt.Sprintf("%s/v%s/%s/%s", GEMINI_API_URL, GEMINI_API_VERSION, GEMINI_TRADES, currency), params)
	trades := []GeminiTrade{}
	err := common.SendHTTPGetRequest(path, true, &trades)
	if err != nil {
		return []GeminiTrade{}, err
	}

	return trades, nil
}

func (g *Gemini) NewOrder(symbol string, amount, price float64, side, orderType string) (int64, error) {
	request := make(map[string]interface{})
	request["symbol"] = symbol
	request["amount"] = strconv.FormatFloat(amount, 'f', -1, 64)
	request["price"] = strconv.FormatFloat(price, 'f', -1, 64)
	request["side"] = side
	request["type"] = orderType

	response := GeminiOrder{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_ORDER_NEW, request, &response)
	if err != nil {
		return 0, err
	}
	return response.OrderID, nil
}

func (g *Gemini) CancelOrder(OrderID int64) (GeminiOrder, error) {
	request := make(map[string]interface{})
	request["order_id"] = OrderID

	response := GeminiOrder{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_ORDER_CANCEL, request, &response)
	if err != nil {
		return GeminiOrder{}, err
	}
	return response, nil
}

func (g *Gemini) CancelOrders(sessions bool) ([]GeminiOrderResult, error) {
	response := []GeminiOrderResult{}
	path := GEMINI_ORDER_CANCEL_ALL
	if sessions {
		path = GEMINI_ORDER_CANCEL_SESSION
	}
	err := g.SendAuthenticatedHTTPRequest("POST", path, nil, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (g *Gemini) GetOrderStatus(orderID int64) (GeminiOrder, error) {
	request := make(map[string]interface{})
	request["order_id"] = orderID

	response := GeminiOrder{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_ORDER_STATUS, request, &response)
	if err != nil {
		return GeminiOrder{}, err
	}
	return response, nil
}

func (g *Gemini) GetOrders() ([]GeminiOrder, error) {
	response := []GeminiOrder{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_ORDERS, nil, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (g *Gemini) GetTradeHistory(symbol string, timestamp int64) ([]GeminiTradeHistory, error) {
	request := make(map[string]interface{})
	request["symbol"] = symbol
	request["timestamp"] = timestamp

	response := []GeminiTradeHistory{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_MYTRADES, request, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (g *Gemini) GetBalances() ([]GeminiBalance, error) {
	response := []GeminiBalance{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_BALANCES, nil, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

//GetExchangeAccountInfo : Retrieves balances for all enabled currencies for the Gemini exchange
func (e *Gemini) GetExchangeAccountInfo() (ExchangeAccountInfo, error) {
	var response ExchangeAccountInfo
	response.ExchangeName = e.GetName()
	accountBalance, err := e.GetBalances()
	if err != nil {
		return response, err
	}
	for i := 0; i < len(accountBalance); i++ {
		var exchangeCurrency ExchangeAccountCurrencyInfo
		exchangeCurrency.CurrencyName = accountBalance[i].Currency
		exchangeCurrency.TotalValue = accountBalance[i].Amount
		exchangeCurrency.Hold = accountBalance[i].Available

		response.Currencies = append(response.Currencies, exchangeCurrency)
	}
	return response, nil
}

func (g *Gemini) PostHeartbeat() (bool, error) {
	type Response struct {
		Result bool `json:"result"`
	}

	response := Response{}
	err := g.SendAuthenticatedHTTPRequest("POST", GEMINI_HEARTBEAT, nil, &response)
	if err != nil {
		return false, err
	}

	return response.Result, nil
}

func (g *Gemini) SendAuthenticatedHTTPRequest(method, path string, params map[string]interface{}, result interface{}) (err error) {
	request := make(map[string]interface{})
	request["request"] = fmt.Sprintf("/v%s/%s", GEMINI_API_VERSION, path)
	request["nonce"] = time.Now().UnixNano()

	if params != nil {
		for key, value := range params {
			request[key] = value
		}
	}

	PayloadJson, err := common.JSONEncode(request)

	if err != nil {
		return errors.New("SendAuthenticatedHTTPRequest: Unable to JSON request")
	}

	if g.Verbose {
		log.Printf("Request JSON: %s\n", PayloadJson)
	}

	PayloadBase64 := common.Base64Encode(PayloadJson)
	hmac := common.GetHMAC(common.HASH_SHA512_384, []byte(PayloadBase64), []byte(g.APISecret))
	headers := make(map[string]string)
	headers["X-GEMINI-APIKEY"] = g.APIKey
	headers["X-GEMINI-PAYLOAD"] = PayloadBase64
	headers["X-GEMINI-SIGNATURE"] = common.HexEncodeToString(hmac)

	resp, err := common.SendHTTPRequest(method, BITFINEX_API_URL+path, headers, strings.NewReader(""))

	if g.Verbose {
		log.Printf("Recieved raw: \n%s\n", resp)
	}

	err = common.JSONDecode([]byte(resp), &result)

	if err != nil {
		return errors.New("Unable to JSON Unmarshal response.")
	}

	return nil
}
