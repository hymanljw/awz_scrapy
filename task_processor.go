package main

import (
	"awesomeProject/db"
	"awesomeProject/proxy"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	logs "github.com/danbai225/go-logs"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

// Task 表示任务的结构体
type Task struct {
	TaskID           string      `json:"task_id"`
	TaskType         string      `json:"task_type"`
	ParseType         string      `json:"parse_type"`
	Keyword          string      `json:"keyword"`
	ASIN             string      `json:"asin"`
	Category         string      `json:"category"`
	MaxPage          int         `json:"max_page"`
	MinPage          int         `json:"min_page"`
	TotalProducts    interface{} `json:"total_products"`
	Result           []Product   `json:"result"`
	Status           string      `json:"status"`
	Appear           string      `json:"appear"`
	TotalResultCount int         `json:"total_result_count"`
	Code             string      `json:"code"`
	ZipCode          string      `json:"zip_code"`
	TryTimes         int         `json:"try_times"`
}

// Position 表示产品在搜索结果中的位置
type Position struct {
	Page           int `json:"page"`
	Position       int `json:"position"`
	GlobalPosition int `json:"global_position"`
}

// Price 表示产品价格信息
type Price struct {
	Discounted   bool    `json:"discounted"`
	CurrentPrice float64 `json:"current_price"`
	BeforePrice  float64 `json:"before_price"`
}

// Reviews 表示产品评论信息
type Reviews struct {
	TotalReviews int     `json:"total_reviews"`
	Rating       float64 `json:"rating"`
}

// Product 表示产品信息
type Product struct {
	Position     Position `json:"position"`
	ASIN         string   `json:"asin"`
	Price        Price    `json:"price"`
	Reviews      Reviews  `json:"reviews"`
	URL          string   `json:"url"`
	Sponsored    bool     `json:"sponsored"`
	AmazonChoice bool     `json:"amazon_choice"`
	BestSeller   bool     `json:"best_seller"`
	AmazonPrime  bool     `json:"amazon_prime"`
	Title        string   `json:"title"`
	Thumbnail    string   `json:"thumbnail"`
}

// 全局变量
var (
	handlingTasks     = make([]string, 0)
	handledRequests   = make([]string, 0)
	rejectedRequests  = make([]interface{}, 0)
	handlingTasksLock sync.Mutex

	// Amazon域名映射
	amazonDomains = map[string]string{
		"US": "amazon.com",
		"DE": "amazon.de",
		"UK": "amazon.co.uk",
		"CA": "amazon.ca",
		"JP": "amazon.co.jp",
		"FR": "amazon.fr",
		"IT": "amazon.it",
		"ES": "amazon.es",
		"AU": "amazon.com.au",
		"MX": "amazon.com.mx",
	}

	// Amazon默认邮编映射
	amazonZipCodes = map[string]string{
		"US": "10001",    // 纽约
		"DE": "10115",    // 柏林
		"UK": "SW1A 1AA", // 伦敦
		"CA": "M5V 2A8",  // 多伦多
		"JP": "100-0001", // 东京
		"FR": "75001",    // 巴黎
		"IT": "00100",    // 罗马
		"ES": "28001",    // 马德里
		"AU": "2000",     // 悉尼
		"MX": "06000",    // 墨西哥城
		"AE": "00000",    // 迪拜
	}

	// 当前任务的code
	currentTaskCode string
)

// ProcessingTask 处理任务的主函数
func ProcessingTask(task Task) string {
	switch task.TaskType {
	case "search_products":
		// 搜索产品
		task.Result, task.Status = SearchProducts(task)
		// task.Status = "success"

		// 获取当前工作目录
		wd, err := os.Getwd()
		if err != nil {
			return "获取工作目录失败"
		}
		// 加载.env文件
		envPath := filepath.Join(wd, ".env")
		err = godotenv.Load(envPath)
		if err != nil {
			return "加载.env文件失败"
		}

		// 根据环境变量决定保存结果的方式
		resultType := os.Getenv("RESULT_TYPE")
		fmt.Println("根据环境变量决定保存结果的方式" + resultType)
		if resultType == "redis" {
			// 保存结果到Redis队列
			err1 := SaveResultsToRedis(&task)
			if err1 != nil {
				logs.Err("保存结果到Redis失败: %v", err1)
			}
		} else if resultType == "mongo" || resultType == "" {
			// 保存结果到MongoDB
			err2 := SaveResultsToMongoDB(task.Result, task.TaskID)
			if err2 != nil {
				logs.Err("保存结果到MongoDB失败: %v", err2)
			}
		}

	case "asin_page":
		// 处理ASIN页面
		task.Status = ASINPage(task)
	case "keyword_appear":
		// 检查关键词出现
		task.Status = KeywordAppear(task)
	default:
		return ""
	}
	return task.Status
}

func createClient() *resty.Client {
	clash := proxy.New("clash.yaml")
	clash.Start()
	// 创建代理URL
	proxyURL, err := url.Parse("http://127.0.0.1:7890")
	if err != nil {
		return nil
	} else {
		fmt.Println(proxyURL.String())
	}
	// 检查端口是否可用
	conn, err := net.DialTimeout("tcp", "127.0.0.1:7890", time.Second*3)
	if err != nil {
		fmt.Println("代理端口7890连接失败:", err)
		return nil
	}
	errCon := conn.Close()
	if errCon != nil {
		return nil
	}
	fmt.Println("代理端口7890连接成功")
	// 创建resty客户端并设置代理
	client := resty.New()
	client.SetProxy("http://127.0.0.1:7890")
	client.SetTimeout(30 * time.Second)
	return client
}

// SearchProducts 处理搜索产品任务
func SearchProducts(task Task) ([]Product, string) {
	var mu sync.Mutex

	kw := task.Keyword
	maxPage := task.MaxPage
	if maxPage == 0 {
		maxPage = 1
	}
	minPage := task.MinPage
	if minPage == 0 {
		minPage = 1
	}

	// 添加到处理中的任务
	handlingTasksLock.Lock()
	handlingTasks = append(handlingTasks, fmt.Sprintf("%s_%s", task.TaskID, task.Keyword))
	handlingTasksLock.Unlock()

	// 创建HTTP客户端
	client := createClient()

	// 设置当前任务的code
	currentTaskCode = task.Code
	amazonDomain := GetAmazonDomain(currentTaskCode)

	// 如果设置了邮编，先设置亚马逊的邮编
	zipCode := ""
	if task.ZipCode != "" {
		zipCode = task.ZipCode
	} else if task.Code != "" {
		// 如果没有设置邮编但设置了国家代码，使用对应国家的默认邮编
		zipCode = GetAmazonZipCode(task.Code)
	}

	if zipCode != "" {
		err := SetAmazonZipCode(client, amazonDomain, zipCode)
		if err != nil {
			logs.Warn("设置亚马逊邮编失败:", err)
			// 即使设置邮编失败，我们仍然继续爬取
		} else {
			logs.Info("成功设置亚马逊邮编:", zipCode)
		}
	}

	allResults := []Product{}
	currentPage := minPage

	// 构建初始URL - 只请求第一页
	kwSearchURL := fmt.Sprintf("https://www.%s/s?k=%s", amazonDomain, url.QueryEscape(kw))
	if task.Category != "" {
		kwSearchURL += "&i=" + task.Category
	}

	pageCount := 0

	// 循环获取所有页面
	for currentPage <= maxPage && pageCount < maxPage {
		log.Printf("<%s> start search keyword: %s, page: %d, URL: %s",
			time.Now().Format("2006-01-02 15:04:05"), kw, currentPage, kwSearchURL)

		fmt.Println(kwSearchURL)
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		}
		resp, err := client.R().
			SetHeaders(headers).
			Get(kwSearchURL)
		if err != nil {
			log.Printf("[ERROR] <%s> keyword: %s, page: %d, error: %v",
				time.Now().Format("2006-01-02 15:04:05"), task.Keyword, currentPage, err)
			break
		}

		if resp.StatusCode() == 200 {
			respHTML := resp.String()

			// 解析HTML
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(respHTML))
			if err != nil {
				log.Printf("[ERROR] <%s> Failed to parse HTML: %v",
					time.Now().Format("2006-01-02 15:04:05"), err)
				break
			}

			// 提取总结果数
			re := regexp.MustCompile(`"totalResultCount":(\w+.[0-9])`)
			matches := re.FindStringSubmatch(respHTML)
			if len(matches) > 1 {
				mu.Lock()
				if task.TotalProducts == nil {
					task.TotalProducts = matches[1]
				}
				mu.Unlock()
			}

			// 解析产品
			pageResult := ScrapePageProds(doc, currentPage)
			log.Printf("<%s> ======  search keyword: %s, page: %d is done, result length: %d  ======",
				time.Now().Format("2006-01-02 15:04:05"), kw, currentPage, len(pageResult))

			// 添加到结果集
			allResults = append(allResults, pageResult...)

			// 记录已处理的请求
			StackInHandledRequests(fmt.Sprintf("%s_%d", task.Keyword, currentPage))

			// 更新页码和计数器
			currentPage++
			pageCount++

			// 检查是否已达到最大页数
			if currentPage > maxPage {
				break
			}

			// 从页面中提取下一页链接
			nextPageLink := doc.Find(".s-pagination-item.s-pagination-next:not(.s-pagination-disabled)")
			if nextPageLink.Length() == 0 {
				// 没有下一页按钮，结束循环
				break
			}

			// 获取下一页的实际URL
			nextPageHref, exists := nextPageLink.Attr("href")
			if exists {
				fmt.Println(nextPageHref)
				// 使用从页面中提取的下一页链接
				if strings.HasPrefix(nextPageHref, "/") {
					// 相对URL，需要添加域名
					kwSearchURL = fmt.Sprintf("https://www.%s%s", amazonDomain, nextPageHref)
				} else if strings.HasPrefix(nextPageHref, "http") {
					// 完整URL，直接使用
					kwSearchURL = nextPageHref
				} else {
					// 其他情况，构建完整URL
					kwSearchURL = fmt.Sprintf("https://www.%s/%s", amazonDomain, nextPageHref)
				}
			} else {
				// 如果无法获取href属性，使用默认构建的URL
				kwSearchURL = fmt.Sprintf("https://www.%s/s?k=%s&page=%d",
					amazonDomain, url.QueryEscape(kw), currentPage)
				if task.Category != "" {
					kwSearchURL += "&i=" + task.Category
				}
			}

		} else if resp.StatusCode() == 503 {
			PushRejectedRequests(resp)
			log.Printf("Error: %d", resp.StatusCode())
			break
		} else {
			log.Printf("Error: %d", resp.StatusCode())
			break
		}
	}

	// 更新任务状态
	task.Result = allResults
	if len(allResults) > 0 {
		fmt.Println(allResults)
		task.Status = "success"
	} else {
		task.Status = "error"
	}

	log.Printf("<%s> ======  task keyword: %s is complete, max_page: %d, total result length: %d, status: %s  ======",
		time.Now().Format("2006-01-02 15:04:05"), task.Keyword, maxPage, len(task.Result), task.Status)

	// 发送结果到回调URL
	//taskJSON, _ := json.Marshal(task)
	//payload := map[string]string{"task": string(taskJSON)}
	//payloadJSON, _ := json.Marshal(payload)
	//
	//_, err := client.R().SetBody(string(payloadJSON)).Post(GetEntrancePoints().CallbackURL)
	//if err != nil {
	//	log.Printf("[ERROR] Failed to send result: %v", err)
	//}

	// 清理任务
	//PopHandlingTask()

	return allResults, task.Status
}

// ScrapePageProds 解析页面中的产品信息
func ScrapePageProds(doc *goquery.Document, page int) []Product {
	prodList := []Product{}

	try := func() {
		eleSearchResults := doc.Find(".s-search-results [data-component-type=\"s-search-result\"]")
		prodsCount := eleSearchResults.Length()
		globalPosition := prodsCount * (page - 1)

		eleSearchResults.Each(func(idx int, item *goquery.Selection) {
			prodItem := Product{}

			// 解析价格
			elePrice := item.Find("span[data-a-size=\"xl\"]").First()
			if elePrice.Length() == 0 {
				elePrice = item.Find("span[data-a-size=\"l\"]").First()
			}
			if elePrice.Length() == 0 {
				elePrice = item.Find("span[data-a-size=\"m\"]").First()
			}

			eleDiscounted := item.Find("span.a-price.a-text-price")
			currentPriceText := ""
			if elePrice.Length() > 0 {
				currentPriceText = elePrice.Find("span.a-offscreen").Text()
			}

			discountPriceText := ""
			if eleDiscounted.Length() > 0 {
				discountPriceText = eleDiscounted.Find("span.a-offscreen").Text()
			}

			// 解析产品链接
			eleProdLink := item.Find("span[data-component-type=\"s-product-image\"] a")
			productURL := ""
			if eleProdLink.Length() > 0 {
				productURL, _ = eleProdLink.Attr("href")
			}

			// 解析评论
			eleReviews := item.Find("[data-csa-c-slot-id=\"alf-reviews\"] a")
			reviewsText := ""
			if eleReviews.Length() > 0 {
				reviewsText, _ = eleReviews.Attr("aria-label")
			}

			// 解析星级
			eleStar := item.Find("[data-cy=\"reviews-block\"] a.a-popover-trigger")
			starText := ""
			if eleStar.Length() > 0 {
				starText, _ = eleStar.Attr("aria-label")
			}

			// 处理特定地区的格式
			// 注意：在Go中我们无法直接获取window.location.host，这里需要根据实际情况调整
			// 这里假设我们有一个函数来检查当前是否是德国或意大利站点
			if IsGermanOrItalianSite() {
				currentPriceText = strings.ReplaceAll(currentPriceText, ".", "")
				currentPriceText = strings.ReplaceAll(currentPriceText, ",", ".")
				discountPriceText = strings.ReplaceAll(discountPriceText, ".", "")
				discountPriceText = strings.ReplaceAll(discountPriceText, ",", ".")
				reviewsText = strings.ReplaceAll(reviewsText, ".", "")
				starText = strings.ReplaceAll(starText, ",", ".")
			}

			// 设置位置信息
			prodItem.Position = Position{
				Page:           page,
				Position:       idx + 1,
				GlobalPosition: globalPosition + idx + 1,
			}

			// 设置ASIN
			prodItem.ASIN, _ = item.Attr("data-asin")

			// 设置价格信息
			currentPrice := 0.0
			if currentPriceText != "" {
				re := regexp.MustCompile(`[^\d.]`)
				currentPriceClean := re.ReplaceAllString(currentPriceText, "")
				currentPrice, _ = strconv.ParseFloat(currentPriceClean, 64)
			}

			beforePrice := 0.0
			if discountPriceText != "" {
				re := regexp.MustCompile(`[^\d.]`)
				beforePriceClean := re.ReplaceAllString(discountPriceText, "")
				beforePrice, _ = strconv.ParseFloat(beforePriceClean, 64)
			}

			prodItem.Price = Price{
				Discounted:   eleDiscounted.Length() > 0,
				CurrentPrice: currentPrice,
				BeforePrice:  beforePrice,
			}

			// 设置评论信息
			totalReviews := 0
			if reviewsText != "" {
				re := regexp.MustCompile(`\d`)
				digits := re.FindAllString(reviewsText, -1)
				totalReviews, _ = strconv.Atoi(strings.Join(digits, ""))
			}

			rating := 0.0
			if starText != "" {
				re := regexp.MustCompile(`\d+\.?\d*`)
				matchStar := re.FindString(starText)
				if matchStar != "" {
					rating, _ = strconv.ParseFloat(matchStar, 64)
				}
			}

			prodItem.Reviews = Reviews{
				TotalReviews: totalReviews,
				Rating:       rating,
			}

			// 设置URL
			amazonDomain := GetAmazonDomain(currentTaskCode)
			if productURL != "" {
				prodItem.URL = fmt.Sprintf("https://www.%s%s", amazonDomain, productURL)
			} else {
				prodItem.URL = fmt.Sprintf("https://www.%s/dp/%s", amazonDomain, prodItem.ASIN)
			}

			// 设置其他属性
			prodItem.Sponsored = item.Find("span.puis-sponsored-label-info-icon").Length() > 0 || strings.Contains(prodItem.URL, "/sspa/")
			prodItem.AmazonChoice = item.Find(fmt.Sprintf("span[id='%s-amazons-choice']", prodItem.ASIN)).Length() > 0
			prodItem.BestSeller = item.Find(fmt.Sprintf("span[id='%s-best-seller']", prodItem.ASIN)).Length() > 0
			prodItem.AmazonPrime = item.Find(".s-prime").Length() > 0

			// 设置标题
			eleTitle := item.Find("[data-cy=\"title-recipe\"] span.a-text-normal")
			if eleTitle.Length() == 0 {
				eleTitle = item.Find("[data-cy=\"title-recipe\"] h2.a-size-base-plus span")
			}
			if eleTitle.Length() > 0 {
				prodItem.Title = eleTitle.Text()
			}

			// 设置缩略图
			eleThumbnail := item.Find("img[data-image-source-density=\"1\"]")
			if eleThumbnail.Length() > 0 {
				prodItem.Thumbnail, _ = eleThumbnail.Attr("src")
			}

			prodList = append(prodList, prodItem)
		})
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] <scrape_page_prods> %v", r)
		}
	}()

	try()

	return prodList
}

// ASINPage 处理ASIN页面任务
func ASINPage(task Task) string {
	// 添加到处理中的任务
	handlingTasksLock.Lock()
	handlingTasks = append(handlingTasks, fmt.Sprintf("%s_%s", task.TaskID, task.ASIN))
	handlingTasksLock.Unlock()

	// 创建HTTP客户端
	client := createClient()

	status := "error"

	try := func() string {
		log.Printf("<%s> start fetch asin page asin: %s", time.Now().Format("2006-01-02 15:04:05"), task.ASIN)

		// 获取对应的Amazon域名
		amazonDomain := GetAmazonDomain(task.Code)

		// 如果设置了邮编，先设置亚马逊的邮编
		zipCode := ""
		if task.ZipCode != "" {
			zipCode = task.ZipCode
		} else if task.Code != "" {
			// 如果没有设置邮编但设置了国家代码，使用对应国家的默认邮编
			zipCode = GetAmazonZipCode(task.Code)
		}

		if zipCode != "" {
			err := SetAmazonZipCode(client, amazonDomain, zipCode)
			if err != nil {
				logs.Warn("设置亚马逊邮编失败:", err)
				// 即使设置邮编失败，我们仍然继续爬取
			} else {
				logs.Info("成功设置亚马逊邮编:", zipCode)
			}
		}

		// 构建ASIN页面URL
		asinURL := fmt.Sprintf("https://www.%s/dp/%s", amazonDomain, task.ASIN)
		headers := map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		}
		resp, err := client.R().SetHeaders(headers).Get(asinURL)

		if err != nil {
			log.Printf("[ERROR] <%s> asin: %s, error: %v", time.Now().Format("2006-01-02 15:04:05"), task.ASIN, err)
			return "error"
		}

		if resp.StatusCode() == 200 {
			// 解析HTML
			_, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
			if err != nil {
				log.Printf("[ERROR] <%s> Failed to parse HTML: %v", time.Now().Format("2006-01-02 15:04:05"), err)
				return "error"
			}

			log.Printf("<%s> ======  search asin: %s is done  ======", time.Now().Format("2006-01-02 15:04:05"), task.ASIN)
			fmt.Println(resp.String())
			return "success"
		} else if resp.StatusCode() == 503 {
			PushRejectedRequests(resp)
			log.Printf("Error: %d", resp.StatusCode())
			return "error"
		} else {
			log.Printf("Error: %d", resp.StatusCode())
			return "error"
		}
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] <%s> asin: %s, panic: %v", time.Now().Format("2006-01-02 15:04:05"), task.ASIN, r)
		}
		StackInHandledRequests(fmt.Sprintf("asin_page_%s", task.ASIN))
		PopHandlingTask()
	}()

	status = try()

	// 更新任务状态
	task.Status = status
	log.Printf("<%s> ======  task asin: %s is complete, status: %s  ======",
		time.Now().Format("2006-01-02 15:04:05"), task.ASIN, task.Status)

	return status
}

// KeywordAppear 处理关键词出现任务
func KeywordAppear(task Task) string {
	// 添加到处理中的任务
	handlingTasksLock.Lock()
	handlingTasks = append(handlingTasks, fmt.Sprintf("%s_%s_%s", task.TaskID, task.Keyword, task.ASIN))
	handlingTasksLock.Unlock()

	// 创建HTTP客户端
	client := resty.New()
	client.SetTimeout(30 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	status := "error"

	try := func() string {
		log.Printf("<%s> start keyword_appear keyword: %s, asin: %s", time.Now().Format("2006-01-02 15:04:05"), task.Keyword, task.ASIN)

		// 获取对应的Amazon域名
		amazonDomain := GetAmazonDomain(task.Code)

		appearURL := fmt.Sprintf("https://www.%s/s?k=%s&field-asin=%s", amazonDomain, url.QueryEscape(task.Keyword), task.ASIN)
		resp, err := client.R().Get(appearURL)

		if err != nil {
			log.Printf("[ERROR] <%s> keyword: %s, asin: %s, error: %v", time.Now().Format("2006-01-02 15:04:05"), task.Keyword, task.ASIN, err)
			return "error"
		}

		if resp.StatusCode() == 200 {
			// 解析HTML
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
			if err != nil {
				log.Printf("[ERROR] <%s> Failed to parse HTML: %v", time.Now().Format("2006-01-02 15:04:05"), err)
				return "error"
			}

			// 检查是否有结果
			searchResultsText := doc.Find("[data-component-type='s-search-results']").Text()
			isNoResult := strings.Contains(searchResultsText, "No results for") ||
				strings.Contains(searchResultsText, "Aucun résultat pour") ||
				strings.Contains(searchResultsText, "Keine Ergebnisse für") ||
				strings.Contains(searchResultsText, "Nessun risultato per") ||
				strings.Contains(searchResultsText, "No hay resultados para") ||
				strings.Contains(searchResultsText, "没有") ||
				strings.Contains(searchResultsText, "の検索に一致する商品はありませんでした")

			searchResultsSize := doc.Find(".s-search-results [data-component-type='s-search-result']").Length()

			if isNoResult {
				task.Appear = "N"
				task.TotalResultCount = 0
			} else {
				task.Appear = "Y"
				task.TotalResultCount = searchResultsSize
			}

			log.Printf("<%s> ======  search keyword: %s, asin: %s is done  ======", time.Now().Format("2006-01-02 15:04:05"), task.Keyword, task.ASIN)
			return "success"
		} else if resp.StatusCode() == 503 {
			PushRejectedRequests(resp)
			log.Printf("Error: %d", resp.StatusCode())
			return "error"
		} else {
			log.Printf("Error: %d", resp.StatusCode())
			return "error"
		}
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] <%s> keyword: %s, asin: %s, panic: %v", time.Now().Format("2006-01-02 15:04:05"), task.Keyword, task.ASIN, r)
		}
		StackInHandledRequests(fmt.Sprintf("keyword_appear_%s_%s", task.Keyword, task.ASIN))
		PopHandlingTask()
	}()

	status = try()

	// 更新任务状态
	task.Status = status
	log.Printf("<%s> ======  task keyword: %s, asin: %s is complete, status: %s, appear: %s  ======",
		time.Now().Format("2006-01-02 15:04:05"), task.Keyword, task.ASIN, task.Status, task.Appear)

	return status
}

// StackInHandledRequests 添加到已处理请求
func StackInHandledRequests(key string) {
	handlingTasksLock.Lock()
	defer handlingTasksLock.Unlock()
	handledRequests = append(handledRequests, key)
}

// PushRejectedRequests 添加到被拒绝请求
func PushRejectedRequests(resp *resty.Response) {
	handlingTasksLock.Lock()
	defer handlingTasksLock.Unlock()
	rejectedRequests = append(rejectedRequests, resp)
}

// PopHandlingTask 移除处理中的任务
func PopHandlingTask() {
	handlingTasksLock.Lock()
	defer handlingTasksLock.Unlock()
	if len(handlingTasks) > 0 {
		handlingTasks = handlingTasks[1:]
	}
}

// IsGermanOrItalianSite 检查是否是德国或意大利站点
func IsGermanOrItalianSite() bool {
	return currentTaskCode == "DE" || currentTaskCode == "IT"
}

// 主函数示例
func main() {
	// if main1() {
	// 	return
	// }
	// 在需要更新配置的地方调用
	// err := proxy.UpdateClashConfig()
	// if err != nil {
	// 	logs.Err("更新Clash配置失败: %v", err)
	// }

	// 解析命令行参数
	taskID := flag.String("id", "", "任务ID")
	taskType := flag.String("type", "", "任务类型: search_products, asin_page, keyword_appear")
	keyword := flag.String("keyword", "", "搜索关键词")
	asin := flag.String("asin", "", "产品ASIN")
	category := flag.String("category", "", "产品类别")
	maxPage := flag.Int("max", 1, "最大页数")
	minPage := flag.Int("min", 1, "最小页数")
	code := flag.String("code", "US", "国家代码: US, DE, UK, CA, JP, FR, IT, ES, AU, MX, AE")
	zipCode := flag.String("zipcode", "", "邮政编码，用于设置亚马逊配送地址")
	tryTimes := flag.Int("trytimes", 0, "任务当前已重试次数")

	// 解析命令行参数
	flag.Parse()

	// 验证必要参数
	if *taskID == "" {
		fmt.Println("错误: 必须提供任务ID (--id)")
		flag.Usage()
		return
	}

	// 根据任务类型验证参数
	switch *taskType {
	case "search_products":
		if *keyword == "" {
			fmt.Println("错误: search_products 任务必须提供关键词 (--keyword)")
			flag.Usage()
			return
		}
	case "asin_page":
		if *asin == "" {
			fmt.Println("错误: asin_page 任务必须提供ASIN (--asin)")
			flag.Usage()
			return
		}
	case "keyword_appear":
		if *keyword == "" || *asin == "" {
			fmt.Println("错误: keyword_appear 任务必须提供关键词 (--keyword) 和 ASIN (--asin)")
			flag.Usage()
			return
		}
	default:
		fmt.Printf("错误: 不支持的任务类型: %s\n", *taskType)
		flag.Usage()
		return
	}

	// 创建任务
	task := Task{
		TaskID:   *taskID,
		TaskType: *taskType,
		Keyword:  *keyword,
		ASIN:     *asin,
		Category: *category,
		MaxPage:  *maxPage,
		MinPage:  *minPage,
		Code:     *code,
		ZipCode:  *zipCode,
		TryTimes: *tryTimes,
	}

	// 处理任务
	result := ProcessingTask(task)
	fmt.Println("Task result:", result)
}
func main1() bool {
	//err := proxy.UpdateClashConfig()
	//if err != nil {
	//	logs.Err("更新Clash配置失败: %v", err)
	//}
	task := Task{
		TaskID:   "task123",
		TaskType: "search_products",
		Keyword:  "wireless headphones",
		MaxPage:  1,
		MinPage:  1,
		Code:     "US",
		Category: "aps",
	}
	// 处理任务
	result := ProcessingTask(task)
	fmt.Println("Task result:", result)
	return true
}

// GetAmazonDomain 根据code获取对应的Amazon域名
func GetAmazonDomain(code string) string {
	if domain, ok := amazonDomains[code]; ok {
		return domain
	}
	return "amazon.com" // 默认返回美国站点
}

// GetAmazonZipCode 根据国家代码获取对应的默认邮编
func GetAmazonZipCode(countryCode string) string {
	if zipCode, ok := amazonZipCodes[countryCode]; ok {
		return zipCode
	}
	return "10001" // 默认返回美国纽约邮编
}

// SetAmazonZipCode 设置亚马逊的邮编
func SetAmazonZipCode(client *resty.Client, amazonDomain string, zipCode string) error {
	if zipCode == "" {
		return nil // 如果没有设置邮编，直接返回
	}

	// 构建地址更改URL
	addressChangeURL := fmt.Sprintf("https://www.%s/gp/delivery/ajax/address-change.html", amazonDomain)

	// 构建表单数据
	formData := map[string]string{
		"locationType": "LOCATION_INPUT",
		"zipCode":      zipCode,
		"storeContext": "generic",
		"deviceType":   "web",
		"pageType":     "Gateway",
		"actionSource": "glow",
	}

	// 发送POST请求设置邮编
	resp, err := client.R().
		SetFormData(formData).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "text/html,*/*").
		SetHeader("X-Requested-With", "XMLHttpRequest").
		Post(addressChangeURL)

	if err != nil {
		return fmt.Errorf("设置邮编失败: %v", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("设置邮编请求返回非200状态码: %d", resp.StatusCode())
	}

	return nil
}

// MongoProduct 表示MongoDB中的产品格式
type MongoProduct struct {
	Position     MongoPosition `json:"position" bson:"position"`
	Price        MongoPrice    `json:"price" bson:"price"`
	Reviews      MongoReviews  `json:"reviews" bson:"reviews"`
	AmazonPrime  bool          `json:"amazon_prime" bson:"amazon_prime"`
	Title        string        `json:"title" bson:"title"`
	CreatedAt    time.Time     `json:"created_at" bson:"created_at"`
	ASIN         string        `json:"asin" bson:"asin"`
	URL          string        `json:"url" bson:"url"`
	Sponsored    bool          `json:"sponsored" bson:"sponsored"`
	AmazonChoice bool          `json:"amazon_choice" bson:"amazon_choice"`
	BestSeller   bool          `json:"best_seller" bson:"best_seller"`
	Thumbnail    string        `json:"thumbnail" bson:"thumbnail"`
	TaskID       string        `json:"task_id" bson:"task_id"`
}

// MongoPosition 表示MongoDB中的位置信息
type MongoPosition struct {
	Page           int `json:"page" bson:"page"`
	Position       int `json:"position" bson:"position"`
	GlobalPosition int `json:"global_position" bson:"global_position"`
}

// MongoPrice 表示MongoDB中的价格信息
type MongoPrice struct {
	Discounted   bool     `json:"discounted" bson:"discounted"`
	CurrentPrice float64  `json:"current_price" bson:"current_price"`
	BeforePrice  *float64 `json:"before_price" bson:"before_price"`
}

// MongoReviews 表示MongoDB中的评论信息
type MongoReviews struct {
	Rating       float64 `json:"rating" bson:"rating"`
	TotalReviews int     `json:"total_reviews" bson:"total_reviews"`
}

// SaveResultsToMongoDB 将结果保存到MongoDB
func SaveResultsToMongoDB(products []Product, taskID string) error {
	// 创建数据库连接
	postgresDB, _, err := db.NewPostgresDB()
	if err != nil {
		return fmt.Errorf("创建数据库连接失败: %v", err)
	}
	defer postgresDB.Close()

	// 从数据库获取MongoDB连接字符串
	mongoURL, err := postgresDB.GetMongoConfig()
	if err != nil {
		return fmt.Errorf("获取MongoDB连接字符串失败: %v", err)
	}
	fmt.Println("<UNK>MongoDB<UNK>:", mongoURL)
	// 创建MongoDB客户端
	clientOptions := options.Client().ApplyURI(mongoURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("连接MongoDB失败: %v", err)
	}
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			logs.Err("断开MongoDB连接失败: %v", err)
		}
	}()

	// 检查连接
	err = client.Ping(ctx, nil)
	if err != nil {
		return fmt.Errorf("MongoDB连接测试失败: %v", err)
	}

	// 获取数据库和集合
	// 从连接字符串中提取数据库名称
	databaseName := extractDatabaseName(mongoURL)
	if databaseName == "" {
		databaseName = "amazon_scraper" // 默认数据库名
	}

	database := client.Database(databaseName)
	collection := database.Collection(taskID)

	// 转换产品格式并插入数据库
	var mongoProducts []interface{}
	for _, product := range products {
		// 处理BeforePrice为null的情况
		var beforePrice *float64
		if product.Price.BeforePrice > 0 {
			bp := product.Price.BeforePrice
			beforePrice = &bp
		}

		mongoProduct := MongoProduct{
			Position: MongoPosition{
				Page:           product.Position.Page,
				Position:       product.Position.Position,
				GlobalPosition: product.Position.GlobalPosition,
			},
			Price: MongoPrice{
				Discounted:   product.Price.Discounted,
				CurrentPrice: product.Price.CurrentPrice,
				BeforePrice:  beforePrice,
			},
			Reviews: MongoReviews{
				Rating:       product.Reviews.Rating,
				TotalReviews: product.Reviews.TotalReviews,
			},
			AmazonPrime:  product.AmazonPrime,
			Title:        product.Title,
			CreatedAt:    time.Now(),
			ASIN:         product.ASIN,
			URL:          product.URL,
			Sponsored:    product.Sponsored,
			AmazonChoice: product.AmazonChoice,
			BestSeller:   product.BestSeller,
			Thumbnail:    product.Thumbnail,
		}

		mongoProducts = append(mongoProducts, mongoProduct)
	}

	// 插入数据
	if len(mongoProducts) > 0 {
		_, err = collection.InsertMany(ctx, mongoProducts)
		if err != nil {
			return fmt.Errorf("插入数据到MongoDB失败: %v", err)
		}
		logs.Info("成功将%d条产品数据保存到MongoDB集合%s", len(mongoProducts), taskID)
	}

	return nil
}

// extractDatabaseName 从MongoDB连接字符串中提取数据库名称
func extractDatabaseName(mongoURL string) string {
	// 简单解析MongoDB连接字符串，提取数据库名
	parts := strings.Split(mongoURL, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// 处理可能包含参数的情况
		if strings.Contains(lastPart, "?") {
			return strings.Split(lastPart, "?")[0]
		}
		return lastPart
	}
	return ""
}

// SaveResultsToRedis 将结果保存到Redis队列
// 新的Redis结果格式，匹配sample.json的格式
type RedisResult struct {
	TaskID        interface{} `json:"task_id"`
	Country       string      `json:"country"`
	MinPage       int         `json:"min_page"`
	MaxPage       int         `json:"max_page"`
	Category      string      `json:"category"`
	TaskType      string      `json:"task_type"`
	Brand         string      `json:"brand"`
	ASIN          string      `json:"asin"`
	ParseType     string      `json:"parse_type"`
	Postcode      string      `json:"postcode"`
	TaskKey       string      `json:"task_key"`
	QueueKey      string      `json:"queue_key"`
	Keyword       string      `json:"keyword"`
	TotalProducts interface{} `json:"total_products"`
	Result        []Product   `json:"result"`
	Status	      string      `json:"status"`
	TryTimes      int         `json:"try_times"`
}

func SaveResultsToRedis(task *Task) error {
	// 创建数据库连接
	postgresDB, _, err := db.NewPostgresDB()
	if err != nil {
		return fmt.Errorf("创建数据库连接失败: %v", err)
	}
	defer postgresDB.Close()

	// 从数据库获取Redis连接字符串
	redisURL, err := postgresDB.GetRedisConfig()
	if err != nil {
		return fmt.Errorf("获取Redis连接字符串失败: %v", err)
	}

	// 创建Redis客户端
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("解析Redis连接字符串失败: %v", err)
	}

	// 确保认证信息正确设置
	// 如果连接字符串中包含用户名和密码，但ParseURL没有正确解析，手动设置
	if opts.Password == "" && strings.Contains(redisURL, "@") {
		fmt.Printf("原始Redis连接字符串: %s\n", redisURL)
		fmt.Printf("ParseURL后的选项: Username=%s, Password=%s\n", opts.Username, opts.Password)

		// 尝试从URL中提取认证信息
		parts := strings.Split(redisURL, "@")
		if len(parts) >= 2 {
			protocolParts := strings.Split(parts[0], "://")
			if len(protocolParts) >= 2 {
				auth := protocolParts[1]
				if strings.Contains(auth, ":") {
					// 格式: username:password
					credentials := strings.Split(auth, ":")
					if len(credentials) == 2 {
						opts.Username = credentials[0]
						opts.Password = credentials[1]
					}
				} else {
					// 格式: redis://username@host:port/db
					// 在这种情况下，username就是密码
					opts.Password = auth
					fmt.Printf("设置密码为: %s\n", auth)
				}
			}
		}
	}

	client := redis.NewClient(opts)
	ctx := context.Background()

	// 检查连接
	_, err = client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("Redis连接测试失败: %v", err)
	}
	defer func(client *redis.Client) {
		errClose := client.Close()
		if errClose != nil {

		}
	}(client)

	// 获取Redis队列名称
	queueName := os.Getenv("REDIS_QUEUE")
	if queueName == "" {
		queueName = "amazon:scraper_task_results" // 默认队列名
	}
	fmt.Println("<UNK>Redis<UNK>:", queueName)
	// 创建新的Redis结果格式
	// 获取任务相关信息
	taskIDValue := task.TaskID

	// 确定parseType
	parseType := "product_shares"
	if task.TaskType != "search_products" {
		parseType = task.TaskType
	}

	// 构建task_key和queue_key
	taskKey := fmt.Sprintf("ads_assembler:amz_scraper_task_%s", task.TaskID)
	queueKey := fmt.Sprintf("amazon:scraper_execute_tasks:%s", task.Code)

	// 创建Redis结果对象
	redisResult := RedisResult{
		TaskID:    taskIDValue,
		Country:   task.Code,
		MinPage:   task.MinPage,
		MaxPage:   task.MaxPage,
		Category:  task.Category,
		TaskType:  task.TaskType,
		Brand:     "",
		ASIN:      task.ASIN,
		ParseType: parseType,
		Postcode: func() string {
			if task.ZipCode != "" {
				return task.ZipCode
			}
			return GetAmazonZipCode(task.Code)
		}(),
		TaskKey:       taskKey,
		QueueKey:      queueKey,
		Keyword:       task.Keyword,
		TotalProducts: len(task.Result),
		Result:        task.Result,
		Status:        task.Status,
		TryTimes:      task.TryTimes,
	}

	// 转换为JSON
	jsonData, err := json.Marshal(redisResult)
	if err != nil {
		return fmt.Errorf("转换Redis结果为JSON失败: %v", err)
	}

	// 添加到Redis队列
	err = client.RPush(ctx, queueName, jsonData).Err()
	if err != nil {
		return fmt.Errorf("添加数据到Redis队列失败: %v", err)
	}

	logs.Info("成功将%d条产品数据保存到Redis队列%s", len(task.Result), queueName)
	return nil
}
