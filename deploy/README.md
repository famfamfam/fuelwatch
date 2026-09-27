# Деплой

Один сервер с Docker, домен указывает на него (A-запись).

```bash
cd deploy
cp .env.example .env         # заполнить DOMAIN, PUBLIC_URL, POSTGRES_PASSWORD, ADMIN_*
```

## Первый запуск: сертификат

nginx не стартует без сертификата, поэтому первый раз сертификат выпускается отдельно (порт 80 должен быть свободен):

```bash
docker compose run --rm -p 80:80 --entrypoint certbot certbot \
  certonly --standalone -d "$(grep ^DOMAIN= .env | cut -d= -f2)" --agree-tos -m you@example.com -n
docker compose up -d --build
```

Дальше контейнер `certbot` продлевает сертификат сам (webroot). Чтобы nginx подхватил новый сертификат: `docker compose exec nginx nginx -s reload` (раз в 1–2 месяца, можно cron).

## Обновление

```bash
git pull && docker compose up -d --build fuelwatch
```

## Проверки и бэкап

- `https://<домен>/healthz` — подключите к любому внешнему uptime-сервису: если сервер лежит, уведомлений не будет.
- Бэкап раз в сутки — `deploy/backup.sh` (дамп БД + текущие эталоны, хранит 14 дней):
  ```bash
  chmod +x backup.sh
  crontab -e   # 30 3 * * *  cd /opt/fuelwatch/deploy && ./backup.sh >> backup.log 2>&1
  ```
  Восстановление: `gunzip -c backups/db-ДАТА.sql.gz | docker compose exec -T postgres psql -U fuelwatch fuelwatch`,
  эталоны — `docker compose exec -T fuelwatch tar xzf - -C /data/frames < backups/references-ДАТА.tgz`.
- Кадры лежат в volume `frames` (`/data/frames`), старые удаляются сами по настройкам `storage.*`.
