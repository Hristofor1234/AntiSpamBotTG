import os
import logging
import psycopg2
from dotenv import load_dotenv
from telegram import Update, BotCommand
from telegram.ext import Application, CommandHandler, MessageHandler, CallbackContext, filters
from telegram.error import BadRequest

# === Настройки ===
load_dotenv()
logging.basicConfig(format='%(asctime)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

ALLOWED_USERS = ["khristo_01"]

# === Подключение к PostgreSQL ===
def get_conn():
    return psycopg2.connect(
        host=os.getenv("DB_HOST"),
        port=os.getenv("DB_PORT"),
        dbname=os.getenv("DB_NAME"),
        user=os.getenv("DB_USER"),
        password=os.getenv("DB_PASSWORD")
    )

# === Работа с БД ===
def init_db():
    with get_conn() as conn, conn.cursor() as cur:
        cur.execute("""
            CREATE TABLE IF NOT EXISTS spam_keywords (
                id SERIAL PRIMARY KEY,
                keyword TEXT UNIQUE NOT NULL
            );
        """)
        conn.commit()

def add_to_db(keyword):
    with get_conn() as conn, conn.cursor() as cur:
        cur.execute("INSERT INTO spam_keywords (keyword) VALUES (%s) ON CONFLICT DO NOTHING", (keyword,))
        conn.commit()

def remove_from_db(keyword):
    with get_conn() as conn, conn.cursor() as cur:
        cur.execute("DELETE FROM spam_keywords WHERE keyword = %s", (keyword,))
        conn.commit()

def get_all_keywords():
    with get_conn() as conn, conn.cursor() as cur:
        cur.execute("SELECT keyword FROM spam_keywords")
        return [row[0] for row in cur.fetchall()]

# === Проверка доступа ===
def is_user_allowed(username):
    return username in ALLOWED_USERS

def check_access(func):
    async def wrapper(update: Update, context: CallbackContext):
        username = update.message.from_user.username
        if not username or not is_user_allowed(username):
            await update.message.reply_text("У вас нет доступа.")
            return
        await func(update, context)
    return wrapper

# === Команды ===
@check_access
async def add_keyword(update: Update, context: CallbackContext):
    added = []
    for kw in [w.lower() for w in context.args]:
        add_to_db(kw)
        added.append(kw)
    if added:
        await update.message.reply_text(f"Добавлены: {', '.join(added)}")
    else:
        await update.message.reply_text("Нечего добавлять.")

@check_access
async def remove_keyword(update: Update, context: CallbackContext):
    removed = []
    for kw in [w.lower() for w in context.args]:
        remove_from_db(kw)
        removed.append(kw)
    if removed:
        await update.message.reply_text(f"Удалены: {', '.join(removed)}")
    else:
        await update.message.reply_text("Нечего удалять.")

@check_access
async def list_keywords(update: Update, context: CallbackContext):
    keywords = get_all_keywords()
    if keywords:
        await update.message.reply_text("Слова:\n" + ", ".join(keywords))
    else:
        await update.message.reply_text("Список пуст.")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    await update.message.reply_text(
        "/add <слова> — добавить\n"
        "/remove <слова> — удалить\n"
        "/list — показать список\n"
        "/commands — команды"
    )

# === Фильтр ===
async def filter_spam(update: Update, context: CallbackContext):
    text = update.message.text.lower()
    for word in get_all_keywords():
        if word in text:
            try:
                await update.message.delete()
                logger.info(f"Удалено сообщение: {text}")
            except BadRequest:
                logger.warning("Нельзя удалить сообщение")
            break

# === Telegram команды ===
async def setup_commands(app):
    await app.bot.set_my_commands([
        BotCommand("add", "Добавить ключевое слово"),
        BotCommand("remove", "Удалить ключевое слово"),
        BotCommand("list", "Показать список"),
        BotCommand("commands", "Показать все команды")
    ])

# === Главная точка входа ===
async def main():
    init_db()

    token = os.getenv("TELEGRAM_BOT_TOKEN")
    webhook_url = os.getenv("WEBHOOK_URL")  # Пример: https://your-app.up.railway.app/webhook
    port = int(os.getenv("PORT", "8080"))

    if not token or not webhook_url:
        raise Exception("TELEGRAM_BOT_TOKEN или WEBHOOK_URL не указаны в .env")

    app = Application.builder().token(token).build()

    app.add_handler(CommandHandler("add", add_keyword))
    app.add_handler(CommandHandler("remove", remove_keyword))
    app.add_handler(CommandHandler("list", list_keywords))
    app.add_handler(CommandHandler("commands", list_commands))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, filter_spam))

    async def post_init(app):
        await app.bot.delete_webhook(drop_pending_updates=True)
        await app.bot.set_webhook(url=webhook_url)
        await setup_commands(app)

    app.post_init = post_init

    await app.run_webhook(
        listen="0.0.0.0",
        port=port,
        webhook_path="/webhook",
    )

if __name__ == "__main__":
    import asyncio
    asyncio.run(main())
