package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
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

type request struct {
	OrderType string  `json:"ordertype"`
	Type      string  `json:"type"`
	Volume    float64 `json:"volume"`
	Pair      string  `json:"pair"`
}

type response struct {
	Status string  `json:"status"`
	Vol    float64 `json:"vol"`
	Fee    float64 `json:"fee"`
	Price  float64 `json:"price"`
	Cost   float64 `json:"cost"`
}

var orders = make(map[string]*order)

func newOrder(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		res, _ := json.Marshal(`{"error": ["bad request"]}`)
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	req := request{}
	if err := json.Unmarshal(buf, &req); err != nil {
		res, _ := json.Marshal(`{"error": ["bad request"]}`)
		http.Error(w, string(res), http.StatusInternalServerError)
		return
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
		res, _ := json.Marshal(`{"error": ["bad request"]}`)
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	req := map[string]interface{}{}
	if err := json.Unmarshal(buf, &req); err != nil {
		res, _ := json.Marshal(`{"error": ["bad request"]}`)
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	id, ok := req["txid"].(string)
	if !ok {
		res, _ := json.Marshal(`{"error": ["bad request"]}`)
		http.Error(w, string(res), http.StatusInternalServerError)
		return
	}

	order, ok := orders[id]
	if !ok {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		order.Id: response{
			Status: order.Status,
			Vol:    order.Volume,
			Fee:    order.Fee,
			Price:  order.Price,
			Cost:   order.Cost,
		},
	}
	res, _ := json.Marshal(resp)

	w.WriteHeader(200)
	w.Write(res)
}

func main() {
	http.HandleFunc("/0/private/AddOrder", newOrder)
	http.HandleFunc("/0/private/QueryOrders", queryOrders)
	http.ListenAndServe(":7777", nil)
}

func randomIntInRange(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(int(n.Int64())) + min
}
