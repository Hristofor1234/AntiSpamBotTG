from telegram import Update, BotCommand
from telegram.ext import Application, MessageHandler, filters, CommandHandler, CallbackContext
from telegram.error import BadRequest
import logging
import json
import os
from dotenv import load_dotenv

# Загрузка .env переменных
load_dotenv()

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Константы
KEYWORDS_FILE = "keywords.json"
SPAM_KEYWORDS = []
ALLOWED_USERS = ["khristo_01"]  # укажи нужных пользователей

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
        await update.message.reply_text(f"Добавлены: {', '.join(added_keywords)}")
    else:
        await update.message.reply_text("Все слова уже в списке.")

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
        await update.message.reply_text(f"Удалены: {', '.join(removed_keywords)}")
    else:
        await update.message.reply_text("Слова не найдены в списке.")

@check_access
async def list_keywords(update: Update, context: CallbackContext):
    if SPAM_KEYWORDS:
        await update.message.reply_text("Ключевые слова:\n" + ", ".join(SPAM_KEYWORDS))
    else:
        await update.message.reply_text("Список ключевых слов пуст.")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    await update.message.reply_text(
        "Доступные команды:\n"
        "/add <слово> - Добавить ключевое слово\n"
        "/remove <слово> - Удалить ключевое слово\n"
        "/list - Показать список\n"
        "/commands - Показать это сообщение"
    )

# === Фильтрация спама ===
async def filter_spam(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()
    for keyword in SPAM_KEYWORDS:
        if keyword in message_text:
            try:
                await update.message.delete()
                logger.info(f"Удалено сообщение: {message_text}")
                return
            except BadRequest as e:
                logger.error(f"Ошибка удаления: {e}")
                return

# === Установка команд в Telegram UI ===
async def setup_bot(application):
    await application.bot.set_my_commands([
        BotCommand("add", "Добавить ключевое слово"),
        BotCommand("remove", "Удалить ключевое слово"),
        BotCommand("list", "Показать список"),
        BotCommand("commands", "Показать доступные команды")
    ])

# === Главная точка входа ===
def main():
    load_keywords()

    token = os.getenv("TELEGRAM_BOT_TOKEN")
    if not token:
        logger.error("Не найден TELEGRAM_BOT_TOKEN в .env!")
        return

    application = Application.builder().token(token).build()

    # Обработчики
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))
    application.add_handler(CommandHandler("commands", list_commands))
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, filter_spam))

    # Установка команд
    application.post_init = setup_bot

    # Запуск
    application.run_polling()

if __name__ == "__main__":
    main()
