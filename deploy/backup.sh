#!/bin/sh
# Ежедневный бэкап: дамп Postgres и текущие эталоны устройств.
# cron: 30 3 * * *  cd /opt/fuelwatch/deploy && ./backup.sh >> backup.log 2>&1
set -eu
umask 077 # в дампе хеши паролей и токенов

DIR="${BACKUP_DIR:-./backups}"
KEEP_DAYS="${BACKUP_KEEP_DAYS:-14}"
STAMP="$(date +%F)"
mkdir -p "$DIR"

docker compose exec -T postgres pg_dump -U fuelwatch fuelwatch | gzip > "$DIR/db-$STAMP.sql.gz"

# Эталоны нужны для зон и сравнения; остальные кадры восстанавливать не обязательно.
REFS="$(docker compose exec -T postgres psql -U fuelwatch -d fuelwatch -At \
  -c "SELECT f.path FROM devices d JOIN frames f ON f.id = d.reference_frame_id")"
if [ -n "$REFS" ]; then
  # shellcheck disable=SC2086
  docker compose exec -T fuelwatch tar czf - -C /data/frames $REFS > "$DIR/references-$STAMP.tgz"
fi

find "$DIR" -name 'db-*.sql.gz' -mtime +"$KEEP_DAYS" -delete
find "$DIR" -name 'references-*.tgz' -mtime +"$KEEP_DAYS" -delete
echo "$(date -Is) backup ok: $(ls -1 "$DIR" | wc -l) files"
