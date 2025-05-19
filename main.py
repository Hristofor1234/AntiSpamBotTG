from telegram import Update, BotCommand
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging
import json
import os
from dotenv import load_dotenv

# Загрузка переменных окружения
load_dotenv()

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Файл для хранения ключевых слов
KEYWORDS_FILE = "keywords.json"

# Список ключевых слов
SPAM_KEYWORDS = []

# Разрешённые пользователи
ALLOWED_USERS = ["khristo_01"]

# === Работа с ключевыми словами ===
def save_keywords():
    with open(KEYWORDS_FILE, "w", encoding="utf-8") as file:
        json.dump(SPAM_KEYWORDS, file, ensure_ascii=False, indent=4)

def load_keywords():
    global SPAM_KEYWORDS
    try:
        with open(KEYWORDS_FILE, "r", encoding="utf-8") as file:
            SPAM_KEYWORDS = json.load(file)
    except FileNotFoundError:
        SPAM_KEYWORDS = []

# === Проверка доступа ===
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

# === Команды ===
@check_access
async def add_keyword(update: Update, context: CallbackContext):
    new_keywords = [kw.lower() for kw in context.args]
    added_keywords = []

    for keyword in new_keywords:
        if keyword not in SPAM_KEYWORDS:
            SPAM_KEYWORDS.append(keyword)
            added_keywords.append(keyword)

    if added_keywords:
        save_keywords()
        await update.message.reply_text(f"Ключевые слова добавлены: {', '.join(added_keywords)}")
    else:
        await update.message.reply_text("Все указанные ключевые слова уже существуют.")

@check_access
async def remove_keyword(update: Update, context: CallbackContext):
    remove_keywords = [kw.lower() for kw in context.args]
    removed_keywords = []

    for keyword in remove_keywords:
        if keyword in SPAM_KEYWORDS:
            SPAM_KEYWORDS.remove(keyword)
            removed_keywords.append(keyword)

    if removed_keywords:
        save_keywords()
        await update.message.reply_text(f"Ключевые слова удалены: {', '.join(removed_keywords)}")
    else:
        await update.message.reply_text("Указанные ключевые слова не найдены в списке.")

@check_access
async def list_keywords(update: Update, context: CallbackContext):
    if SPAM_KEYWORDS:
        await update.message.reply_text("Текущие ключевые слова:\n" + ", ".join(SPAM_KEYWORDS))
    else:
        await update.message.reply_text("Список ключевых слов пуст.")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    await update.message.reply_text(
        "Доступные команды:\n"
        "/add <слово> - Добавить ключевое слово\n"
        "/remove <слово> - Удалить ключевое слово\n"
        "/list - Показать список ключевых слов\n"
        "/commands - Показать это сообщение"
    )

# === Фильтрация ===
async def filter_spam(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()
    for keyword in SPAM_KEYWORDS:
        if keyword in message_text:
            try:
                await update.message.delete()
                logger.info(f"Удалено сообщение: {message_text}")
                return
            except BadRequest as e:
                logger.error(f"Ошибка при удалении: {e}")
                return

# === Установка команд для Telegram UI ===
async def set_commands(application):
    await application.bot.set_my_commands([
        BotCommand("add", "Добавить ключевое слово"),
        BotCommand("remove", "Удалить ключевое слово"),
        BotCommand("list", "Показать список ключевых слов"),
        BotCommand("commands", "Показать команды")
    ])

# === Точка входа ===
def main():
    load_keywords()

    token = os.getenv("TELEGRAM_BOT_TOKEN")
    if not token:
        logger.error("Не найден TELEGRAM_BOT_TOKEN в .env!")
        return

    application = Application.builder().token(token).build()

    # Регистрация команд
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))
    application.add_handler(CommandHandler("commands", list_commands))

    # Фильтр сообщений
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, filter_spam))

    # Установка команд и запуск бота
    async def start_bot():
        await set_commands(application)
        await application.run_polling()

    import asyncio
    asyncio.run(start_bot())

# === Запуск ===
if __name__ == "__main__":
    main()
