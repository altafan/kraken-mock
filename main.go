package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

type order struct {
	Id        string
	OrderType string
	Type      string
	Volume    float64
	Pair      string
	Status    string
	Fee       float64
	Price     float64
	Cost      float64
}

func (o *order) String() string {
	return fmt.Sprintf("%s %f %s @ market", o.Type, o.Volume, o.Pair)
}

type newOrderRequest struct {
	OrderType string  `json:"ordertype"`
	Type      string  `json:"type"`
	Volume    float64 `json:"volume"`
	Pair      string  `json:"pair"`
}

func newOrderRequestFromString(str string) newOrderRequest {
	var orderType, typ, pair string
	var volume float64
	args := strings.Split(str, "&")
	for _, arg := range args {
		a := strings.Split(arg, "=")
		switch a[0] {
		case "orderType":
			orderType = a[1]
		case "type":
			typ = a[1]
		case "pair":
			pair = a[1]
		case "volume":
			volume, _ = strconv.ParseFloat(a[1], 64)
		}
	}

	return newOrderRequest{
		OrderType: orderType,
		Type:      typ,
		Volume:    volume,
		Pair:      pair,
	}
}

type newOrderResponse struct {
	Status string  `json:"status"`
	Vol    float64 `json:"vol"`
	Fee    float64 `json:"fee"`
	Price  float64 `json:"price"`
	Cost   float64 `json:"cost"`
}

type queryOrderRequest struct {
	Txid string `json:"txid"`
}

func queryOrderRequestFromString(str string) queryOrderRequest {
	var txid string
	args := strings.Split(str, "&")
	for _, arg := range args {
		a := strings.Split(arg, "=")
		switch a[0] {
		case "txid":
			txid = a[1]
		}
	}

	return queryOrderRequest{Txid: txid}
}

type balanceResponse struct {
	// error : array of strings
	Error  []string           `json:"error"`
	Result map[string]float64 `json:"result"`
}

type getAddrRequest struct {
	Asset  string `json:"asset"`
	Method string `json:"method"`
}

var orders = make(map[string]*order)

func newOrder(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		res, _ := json.Marshal(map[string][]string{"error": {"bad request"}})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	req := newOrderRequest{}
	if err := json.Unmarshal(buf, &req); err != nil {
		req = newOrderRequestFromString(string(buf))
	}

	order := &order{
		Id:        uuid.New().String(),
		Status:    "open",
		OrderType: req.OrderType,
		Pair:      req.Pair,
		Volume:    req.Volume,
		Type:      req.Type,
	}
	orders[order.Id] = order
	resp := make(map[string]interface{})
	resp["result"] = map[string]interface{}{
		"txid":  []string{order.Id},
		"descr": order.String(),
	}
	res, _ := json.Marshal(resp)
	fmt.Printf("new order %s\n", order.Id)
	go closeOrder(order.Id)

	w.WriteHeader(200)
	w.Write(res)
}

func closeOrder(id string) {
	wait := randomIntInRange(1, 5)
	fmt.Printf("completing order %s in %d seconds\n", id, wait)
	time.Sleep(time.Duration(wait) * time.Second)

	pp, err := http.Get(fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", orders[id].Pair))
	if err != nil {
		fmt.Println(err)
		return
	}

	defer pp.Body.Close()
	buf, err := io.ReadAll(pp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	v := map[string]interface{}{}
	json.Unmarshal(buf, &v)

	if a, ok := v["error"].([]interface{}); !ok {
		fmt.Println(a)
		return
	}

	for _, vvv := range v["result"].(map[string]interface{}) {
		i := vvv.(map[string]interface{})
		price, _ := strconv.ParseFloat(i["c"].([]interface{})[0].(string), 64)
		fee := orders[id].Volume * 0.1
		cost := orders[id].Volume * price
		orders[id].Cost = cost
		orders[id].Fee = fee
		orders[id].Price = price
		orders[id].Status = "closed"
	}
	fmt.Printf("order %s closed\n", id)
}

func queryOrders(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		res, _ := json.Marshal(map[string][]string{"error": {"bad request"}})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	req := queryOrderRequest{}
	if err := json.Unmarshal(buf, &req); err != nil {
		req = queryOrderRequestFromString(string(buf))
	}

	order, ok := orders[req.Txid]
	if !ok {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"result": map[string]interface{}{
			order.Id: newOrderResponse{
				Status: order.Status,
				Vol:    order.Volume,
				Fee:    order.Fee,
				Price:  order.Price,
				Cost:   order.Cost,
			},
		},
	}
	res, _ := json.Marshal(resp)

	w.WriteHeader(200)
	w.Write(res)
}

func getBalance(w http.ResponseWriter, r *http.Request) {
	balances, err := getBalancesFromConfig()
	if err != nil {
		res, _ := json.Marshal(map[string]string{"error": err.Error()})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	for k, v := range balances {
		delete(balances, k)
		balances[strings.ToUpper(k)] = v
	}

	response := balanceResponse{
		Error:  []string{},
		Result: balances,
	}

	res, err := json.Marshal(response)
	if err != nil {
		res, _ := json.Marshal(map[string]string{"error": "bad request"})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func getAddress(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		res, _ := json.Marshal(map[string]string{"error": "bad request"})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	body := getAddrRequest{}
	asset := ""
	if err := json.Unmarshal(buf, &body); err != nil {
		reqStr := string(buf)
		s := strings.Split(reqStr, "&")
		a := strings.Split(s[0], "=")
		if len(a) > 1 {
			asset = a[1]
		}
	} else {
		asset = body.Asset
	}

	if asset == "" {
		res, _ := json.Marshal(map[string]string{"error": "missing asset"})
		http.Error(w, string(res), http.StatusBadRequest)
		return
	}

	addr, err := getAddressFromConfig(asset)
	if err != nil {
		res, _ := json.Marshal(map[string]string{"error": err.Error()})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"error": []interface{}{},
		"result": []map[string]string{
			{
				"address": addr,
				"key":     uuid.NewString(),
			},
		},
	}

	res, _ := json.Marshal(response)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func withdraw(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if _, err := io.ReadAll(r.Body); err != nil {
		res, _ := json.Marshal(map[string]string{"error": "bad request"})
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"error": []interface{}{},
		"result": map[string]string{
			"refid": uuid.New().String(),
		},
	}

	res, _ := json.Marshal(response)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func main() {
	fmt.Println("started listening to :7777")
	http.HandleFunc("/0/private/AddOrder", newOrder)
	http.HandleFunc("/0/private/QueryOrders", queryOrders)
	http.HandleFunc("/0/private/Balance", getBalance)
	http.HandleFunc("/0/private/DepositAddresses", getAddress)
	http.HandleFunc("/0/private/WithdrawAddresses", getAddress)
	http.HandleFunc("/0/private/Withdraw", withdraw)
	http.ListenAndServe(":7777", nil)
}

func randomIntInRange(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(int(n.Int64())) + min
}

func getBalancesFromConfig() (map[string]float64, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	balances := make(map[string]float64)
	err = viper.UnmarshalKey("balances", &balances)
	if err != nil {
		return nil, err
	}

	return balances, nil
}

func getAddressFromConfig(asset string) (string, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		return "", err
	}

	addresses := make(map[string]string)
	err = viper.UnmarshalKey("addresses", &addresses)
	if err != nil {
		return "", err
	}

	addr, ok := addresses[strings.ToLower(asset)]
	if !ok {
		return "", fmt.Errorf("address not found for asset %s", asset)
	}

	return addr, nil
}
