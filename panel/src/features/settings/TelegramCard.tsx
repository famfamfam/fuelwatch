import { Send } from "lucide-react";
import { toast } from "sonner";
import { errorMessage } from "@/api/client";
import { useTelegramStatus, useTelegramTest } from "@/api/visits";
import { Button } from "@/components/ui/button";
import { Badge, Card, CardContent } from "@/components/ui/primitives";

/** Telegram — дополнительный канал (D-11): статус токена и тестовое сообщение. */
export function TelegramCard() {
  const st = useTelegramStatus();
  const test = useTelegramTest();
  if (!st.data) return null;
  return (
    <Card>
      <CardContent className="flex flex-wrap items-center justify-between gap-3 pt-4">
        <div className="space-y-1">
          <p className="flex items-center gap-2 text-sm font-medium">
            Telegram-бот
            {st.data.configured ? <Badge tone="ok">токен задан</Badge> : <Badge tone="warning">нет токена</Badge>}
          </p>
          <p className="text-sm text-muted-foreground">
            {st.data.configured
              ? "Добавьте бота в нужные чаты, впишите chat_id ниже и включите отправку. Кнопки 👍/👎/📷 и команды /status, /snapshot работают из этих чатов."
              : "Задайте TELEGRAM_BOT_TOKEN в .env сервера и перезапустите его — тогда здесь появится отправка."}
          </p>
        </div>
        {st.data.configured && (
          <Button
            variant="outline"
            disabled={test.isPending}
            onClick={() =>
              test.mutate(undefined, {
                onSuccess: (r) => toast.success(`Отправлено в ${r.sent} чат(ов)`),
                onError: (e) => toast.error(errorMessage(e)),
              })
            }
          >
            <Send /> Тестовое сообщение
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
