docker build -t awesome .
docker run --rm awesome --id task123 --type search_products --keyword "wireless headphones" --max 1 --min 1 --code US