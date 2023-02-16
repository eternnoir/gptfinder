package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-shiori/go-readability"
	"google.golang.org/api/customsearch/v1"
	"google.golang.org/api/googleapi/transport"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	gapiKey = os.Getenv("GAPI_KEY")
	port    = os.Getenv("PORT")
	cxid    = os.Getenv("CXID")
)

func main() {
	// get evn GAPI_KEY and check must have value
	gapiKey = os.Getenv("GAPI_KEY")
	log.Println(gapiKey)
	if gapiKey == "" {
		fmt.Println("GAPI_KEY must be set")
		os.Exit(1)
	}

	// get evn PORT and check must have value
	port = os.Getenv("PORT")
	if port == "" {
		fmt.Println("PORT must be set")
		os.Exit(1)
	}

	// get ENV CXID and check must have value
	cxid = os.Getenv("CXID")
	if cxid == "" {
		fmt.Println("CXID must be set")
		os.Exit(1)
	}

	// create a new gin engine
	//
	//router := gin.New()
	//router.Use(gin.Recovery())
	////v1 := router.Group("/v1")
	//// get evn USERNAME and check must have value
	//router.GET("/ping", func(c *gin.Context) {
	//	c.JSON(http.StatusOK, gin.H{
	//		"message": "pong",
	//	})
	//})
	//router.GET("/v1/query", GoogleSearch)
	//// create a client
	//if err := router.Run(":" + port); err != nil {
	//	panic(err)
	//}
	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/v1/query", QueryHandler)

	http.ListenAndServe(":"+port, nil)
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Msg string
	}{
		Msg: "pong",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(data)
}
func QueryHandler(w http.ResponseWriter, r *http.Request) {
	// ger q from url params
	q := r.URL.Query().Get("q")
	// ger cx from url params
	limit := r.URL.Query().Get("limit")
	// ger key from url params
	if limit == "" {
		limit = "10"
	}
	// convert limit to int
	limitInt, err := strconv.Atoi(limit)
	if err != nil {

		log.Println(err)
		HandleError(w, http.StatusBadRequest, err)
		return
	}

	client := &http.Client{Transport: &transport.APIKey{Key: gapiKey}}

	svc, err := customsearch.New(client)
	if err != nil {
		log.Println(err)
		HandleError(w, http.StatusBadRequest, err)
	}
	results := []*customsearch.Result{}
	start := 1
	climit := limitInt
	for {
		if start > limitInt {
			break
		}
		leftLimit := limitInt - start + 1
		if leftLimit > 10 {
			climit = 10
		} else {
			climit = leftLimit
		}
		resp, err := svc.Cse.List().Cx(cxid).Q(q).Num(int64(climit)).Start(int64(start)).Do()
		if err != nil {
			log.Println(err)
			HandleError(w, http.StatusBadRequest, err)
			return
		}
		for _, result := range resp.Items {
			// append result to results
			results = append(results, result)
		}
		start += 10
	}
	resp := []*WebPageData{}
	resultch := make(chan *WebPageData)
	var wg sync.WaitGroup
	for _, result := range results {
		wg.Add(1)
		go fetchUrlData(result.FormattedUrl, result.Title, resultch, &wg)
	}
	go func() {
		wg.Wait()
		close(resultch)
	}()
	for res := range resultch {
		resp = append(resp, res)
	}
	ResponseOK(w, resp)
}

func HandleError(w http.ResponseWriter, statusCode int, err error) {
	// use json response err
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": err.Error(),
	})
}

func ResponseOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func GoogleSearch(c *gin.Context) {
	// get q from c.Query
	q := c.Query("q")
	// get limit from c.Query
	limit := c.Query("limit")
	if limit == "" {
		limit = "10"
	}
	// convert limit to int
	limitInt, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "limit must be an integer",
		})
		return
	}

	client := &http.Client{Transport: &transport.APIKey{Key: gapiKey}}

	svc, err := customsearch.New(client)
	if err != nil {
		log.Fatal(err)
	}
	results := []*customsearch.Result{}
	start := 1
	climit := limitInt
	for {
		if start > limitInt {
			break
		}
		leftLimit := limitInt - start + 1
		if leftLimit > 10 {
			climit = 10
		} else {
			climit = leftLimit
		}
		resp, err := svc.Cse.List().Cx(cxid).Q(q).Num(int64(climit)).Start(int64(start)).Do()
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err,
			})
			return
		}
		for _, result := range resp.Items {
			// append result to results
			results = append(results, result)
		}
		start += 10
	}
	resp := []*WebPageData{}
	resultch := make(chan *WebPageData)
	var wg sync.WaitGroup
	for _, result := range results {
		wg.Add(1)
		go fetchUrlData(result.FormattedUrl, result.Title, resultch, &wg)
	}
	go func() {
		wg.Wait()
		close(resultch)
	}()
	for res := range resultch {
		resp = append(resp, res)
	}
	c.JSON(http.StatusOK, resp)
}

type WebPageData struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Href  string `json:"href"`
}

func fetchUrlData(url, title string, ch chan<- *WebPageData, wg *sync.WaitGroup) {
	defer wg.Done()
	article, err := readability.FromURL(url, 30*time.Second)
	if err != nil {
		log.Fatalf("failed to parse %s, %v\n", url, err)
	}

	fmt.Printf("URL     : %s\n", url)
	fmt.Printf("Title   : %s\n", article.Title)
	fmt.Printf("Author  : %s\n", article.Byline)
	fmt.Printf("Length  : %d\n", article.Length)
	fmt.Printf("Excerpt : %s\n", article.Excerpt)
	fmt.Printf("SiteName: %s\n", article.SiteName)
	fmt.Printf("Image   : %s\n", article.Image)
	fmt.Printf("Favicon : %s\n", article.Favicon)
	// print article.Content
	fmt.Printf("Content : %s\n", standardizeSpaces(article.TextContent))
	fmt.Println()
	ch <- &WebPageData{
		Title: title,
		Body:  standardizeSpaces(article.TextContent),
		Href:  url,
	}
}

func standardizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
