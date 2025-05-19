from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging
import os
import psycopg2
from psycopg2.extras import RealDictCursor
from dotenv import load_dotenv

# Загрузка переменных окружения
load_dotenv()

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Список разрешённых пользователей
ALLOWED_USERS = ["khristo_01"]

# Получение подключения к базе данных PostgreSQL
def get_db_connection():
    return psycopg2.connect(
        dbname=os.getenv("DB_NAME"),
        user=os.getenv("DB_USER"),
        password=os.getenv("DB_PASSWORD"),
        host=os.getenv("DB_HOST"),
        port=os.getenv("DB_PORT"),
        cursor_factory=RealDictCursor
    )

# Проверка доступа
def is_user_allowed(username):
    return username in ALLOWED_USERS

def check_access(func):
    async def wrapper(update: Update, context: CallbackContext):
        username = update.message.from_user.username
        if not username or not is_user_allowed(username):
            await update.message.reply_text("У вас нет прав на выполнение этой команды.")
            return
        await func(update, context)
    return wrapper

# Команды управления ключевыми словами
@check_access
async def add_keyword(update: Update, context: CallbackContext):
    new_keywords = [kw.lower() for kw in context.args]
    added_keywords = []

    with get_db_connection() as conn:
        with conn.cursor() as cur:
            for keyword in new_keywords:
                try:
                    cur.execute("INSERT INTO spam_keywords (keyword) VALUES (%s) ON CONFLICT DO NOTHING", (keyword,))
                    if cur.rowcount:
                        added_keywords.append(keyword)
                except Exception as e:
                    logger.error(f"Ошибка добавления слова '{keyword}': {e}")

    if added_keywords:
        await update.message.reply_text(f"Ключевые слова добавлены: {', '.join(added_keywords)}")
    else:
        await update.message.reply_text("Все указанные ключевые слова уже существуют.")

@check_access
async def remove_keyword(update: Update, context: CallbackContext):
    remove_keywords = [kw.lower() for kw in context.args]
    removed_keywords = []

    with get_db_connection() as conn:
        with conn.cursor() as cur:
            for keyword in remove_keywords:
                cur.execute("DELETE FROM spam_keywords WHERE keyword = %s", (keyword,))
                if cur.rowcount:
                    removed_keywords.append(keyword)

    if removed_keywords:
        await update.message.reply_text(f"Ключевые слова удалены: {', '.join(removed_keywords)}")
    else:
        await update.message.reply_text("Указанные ключевые слова не найдены в списке.")

@check_access
async def list_keywords(update: Update, context: CallbackContext):
    with get_db_connection() as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT keyword FROM spam_keywords")
            rows = cur.fetchall()
            keywords = [row['keyword'] for row in rows]

    if keywords:
        await update.message.reply_text("Текущие ключевые слова:\n" + "\n".join(keywords))
    else:
        await update.message.reply_text("Список ключевых слов пуст.")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    await update.message.reply_text(
        "Доступные команды:\n"
        "/add <слово> - Добавить ключевое слово\n"
        "/remove <слово> - Удалить ключевое слово\n"
        "/list - Показать список ключевых слов"
    )

# Фильтрация спама
async def filter_spam(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()

    with get_db_connection() as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT keyword FROM spam_keywords")
            rows = cur.fetchall()
            for row in rows:
                if row["keyword"] in message_text:
                    try:
                        await update.message.delete()
                        logger.info(f"Удалено сообщение: {message_text}")
                        return
                    except BadRequest as e:
                        logger.error(f"Ошибка при удалении: {e}")
                        return

def main():
    # Получение токена из переменной окружения
    token = os.getenv("TELEGRAM_BOT_TOKEN")
    if not token:
        logger.error("TELEGRAM_BOT_TOKEN не найден в .env!")
        return

    application = Application.builder().token(token).build()

    # Обработчики команд
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))
    application.add_handler(CommandHandler("commands", list_commands))

    # Обработчик сообщений
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, filter_spam))

    # Запуск бота
    application.run_polling()

if __name__ == '__main__':
    main()
