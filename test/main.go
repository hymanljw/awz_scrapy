package test

import (
	"awesomeProject/proxy"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Product 结构体用于存储产品信息
type Product struct {
	Asin      string `json:"asin"`
	Title     string `json:"title"`
	Image     string `json:"image"`
	Rating    string `json:"rating"`
	Reviews   string `json:"reviews"`
	Price     string `json:"price"`
	RankPage  int    `json:"rank_page"`
	RankPos   int    `json:"rank_pos"`
	Keyword   string `json:"keyword"` // 添加关键词字段，记录是哪个关键词的搜索结果
	Sponsored string `json:"sponsored"`
	Choice    string `json:"amazon_choice"`
	Best      string `json:"best_seller"`
	Prime     string `json:"amazon_prime"`
}

// Config 结构体用于从JSON文件读取配置
type Config struct {
	Keywords []string `json:"keywords"`
	MaxPages int      `json:"max_pages"`
	SleepSec int      `json:"sleep_sec"`
}

// SearchResult 结构体用于输出结果
type SearchResult struct {
	Keyword  string    `json:"keyword"`
	Products []Product `json:"products"`
	Time     string    `json:"time"`
}

// parseProducts 解析HTML并提取产品信息
func parseProducts(doc *goquery.Document, pageNum int, keyword string) []Product {
	products := []Product{}

	doc.Find("div[data-asin][data-component-type='s-search-result']").Each(func(idx int, s *goquery.Selection) {
		asin, _ := s.Attr("data-asin")

		// 标题
		title := ""
		titleSel := s.Find("h2 span")
		if titleSel.Length() > 0 {
			title = strings.TrimSpace(titleSel.Text())
		}

		// 图片
		imgURL := ""
		imgSel := s.Find("img.s-image")
		if imgSel.Length() > 0 {
			imgURL, _ = imgSel.Attr("src")
		}

		// 评分
		rating := ""
		ratingSel := s.Find("span.a-icon-alt")
		if ratingSel.Length() > 0 {
			ratingText := strings.TrimSpace(ratingSel.Text())
			ratingParts := strings.Split(ratingText, " ")
			if len(ratingParts) > 0 {
				rating = ratingParts[0]
			}
		}

		// 评论数
		reviews := ""
		reviewsSel := s.Find("span[aria-label][class*='s-link-style']")
		if reviewsSel.Length() > 0 {
			reviews = strings.ReplaceAll(strings.TrimSpace(reviewsSel.Text()), ",", "")
		}

		// 价格
		price := ""
		priceSel := s.Find("span.a-price > span.a-offscreen")
		if priceSel.Length() > 0 {
			price = strings.ReplaceAll(strings.TrimSpace(priceSel.Text()), "$", "")
		}

		sponsored := ""
		sponsoredSel := s.Find("span.puis-sponsored-label-info-icon")
		if sponsoredSel.Length() > 0 {
			sponsored = strings.TrimSpace(sponsoredSel.Text())
		}
		// 排名
		rankPage := pageNum
		rankPos := idx + 1

		products = append(products, Product{
			Asin:      asin,
			Title:     title,
			Image:     imgURL,
			Rating:    rating,
			Reviews:   reviews,
			Price:     price,
			RankPage:  rankPage,
			RankPos:   rankPos,
			Keyword:   keyword, // 添加关键词
			Sponsored: sponsored,
		})
	})

	return products
}

// searchAmazon 搜索亚马逊产品
func searchAmazon(keyword string, maxPages int, sleepSec int, client *resty.Client) []Product {
	headers := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}

	baseURL := "https://www.amazon.com/s"
	allProducts := []Product{}

	for page := 1; page <= maxPages; page++ {
		resp, err := client.R().
			SetHeaders(headers).
			SetQueryParams(map[string]string{
				"k":    keyword,
				"page": strconv.Itoa(page),
			}).
			Get(baseURL)

		if err != nil || resp.StatusCode() != 200 {
			fmt.Printf("Failed to fetch page %d for keyword '%s': %v\n", page, keyword, err)
			break
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body())))
		if err != nil {
			fmt.Printf("Failed to parse HTML for page %d: %v\n", page, err)
			break
		}

		products := parseProducts(doc, page, keyword)
		if len(products) == 0 {
			fmt.Printf("No products found on page %d for keyword '%s'\n", page, keyword)
			break
		}

		allProducts = append(allProducts, products...)
		fmt.Printf("Page %d done for keyword '%s', found %d products.\n", page, keyword, len(products))

		// 防止被封
		time.Sleep(time.Duration(sleepSec) * time.Second)
	}

	return allProducts
}

// readConfigFromFile 从JSON文件读取配置
func readConfigFromFile(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}
	defer file.Close()

	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("无法读取配置文件: %v", err)
	}

	var config Config
	err = json.Unmarshal(byteValue, &config)
	if err != nil {
		return nil, fmt.Errorf("无法解析配置JSON: %v", err)
	}

	// 设置默认值
	if config.MaxPages <= 0 {
		config.MaxPages = 1 // 默认搜索1页
	}
	if config.SleepSec <= 0 {
		config.SleepSec = 2 // 默认延迟2秒
	}

	return &config, nil
}

// saveResultsToFile 将结果保存到JSON文件
func saveResultsToFile(results []SearchResult) error {
	// 创建results目录（如果不存在）
	resultsDir := "results"
	if _, err := os.Stat(resultsDir); os.IsNotExist(err) {
		os.Mkdir(resultsDir, 0755)
	}

	// 使用时间戳创建文件名
	timestamp := time.Now().Format("20060102_150405")
	filePath := filepath.Join(resultsDir, fmt.Sprintf("amazon_results_%s.json", timestamp))

	// 将结果转换为JSON
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("无法将结果转换为JSON: %v", err)
	}

	// 写入文件
	err = ioutil.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("无法写入结果文件: %v", err)
	}

	fmt.Printf("结果已保存到: %s\n", filePath)
	return nil
}

func main() {
	// 从JSON文件读取配置
	configFile := "keywords.json"
	config, err := readConfigFromFile(configFile)
	if err != nil {
		fmt.Printf("读取配置失败: %v\n", err)
		fmt.Println("请确保在当前目录下有一个keywords.json文件，格式如下：")
		fmt.Println(`{
  "keywords": ["wireless headphones", "bluetooth speaker", "smart watch"],
  "max_pages": 1,
  "sleep_sec": 2
}`)
		return
	}

	clash := proxy.New("clash.yaml")
	clash.Start()
	// 创建代理URL
	proxyURL, err := url.Parse("http://127.0.0.1:7890")
	if err != nil {
		return
	} else {
		fmt.Println(proxyURL.String())
	}
	// 检查端口是否可用
	conn, err := net.DialTimeout("tcp", "127.0.0.1:7890", time.Second*3)
	if err != nil {
		fmt.Println("代理端口7890连接失败:", err)
		return
	}
	conn.Close()
	fmt.Println("代理端口7890连接成功")
	// 创建resty客户端并设置代理
	client := resty.New()
	client.SetProxy("http://127.0.0.1:7890")
	client.SetTimeout(30 * time.Second)

	// 显示配置信息
	fmt.Printf("加载配置成功，将搜索%d个关键词，每个关键词搜索%d页，请求间隔%d秒\n",
		len(config.Keywords), config.MaxPages, config.SleepSec)

	// 存储所有关键词的搜索结果
	allResults := []SearchResult{}

	// 对每个关键词进行搜索
	for _, keyword := range config.Keywords {
		fmt.Printf("开始搜索关键词: %s\n", keyword)
		products := searchAmazon(keyword, config.MaxPages, config.SleepSec, client)

		// 添加到结果中
		if len(products) > 0 {
			result := SearchResult{
				Keyword:  keyword,
				Products: products,
				Time:     time.Now().Format(time.RFC3339),
			}
			allResults = append(allResults, result)
		}

		// 在不同关键词搜索之间添加更长的延迟
		time.Sleep(time.Duration(config.SleepSec*2) * time.Second)
	}

	// 将结果保存到JSON文件
	if len(allResults) > 0 {
		err := saveResultsToFile(allResults)
		if err != nil {
			fmt.Printf("保存结果失败: %v\n", err)
		}
	} else {
		fmt.Println("没有找到任何产品")
	}
}
