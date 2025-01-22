from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging
import json

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Файл для хранения ключевых слов
KEYWORDS_FILE = "keywords.json"

# Список ключевых слов для фильтрации спама
SPAM_KEYWORDS = []

# Список разрешённых пользователей (username)
ALLOWED_USERS = ["khristo_01"]

# Загрузка и сохранение ключевых слов
def save_keywords():
    with open(KEYWORDS_FILE, "w") as file:
        json.dump(SPAM_KEYWORDS, file)

def load_keywords():
    global SPAM_KEYWORDS
    try:
        with open(KEYWORDS_FILE, "r") as file:
            SPAM_KEYWORDS = json.load(file)
    except FileNotFoundError:
        SPAM_KEYWORDS = []

# Проверка доступа к командам
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
    await update.message.reply_text(f"Текущие ключевые слова: {', '.join(SPAM_KEYWORDS)}")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    await update.message.reply_text(
        "Доступные команды:\n"
        "/add <слово> - Добавить ключевое слово\n"
        "/remove <слово> - Удалить ключевое слово\n"
        "/list - Показать список ключевых слов"
    )

def main():
    load_keywords()
    application = Application.builder().token("7706488866:AAH5rPfgUA0zDY_D3wqbcHc7DAxAfLgxQDE").build()

    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))
    application.add_handler(CommandHandler("commands", list_commands))

    application.run_polling()
    save_keywords()

if __name__ == '__main__':
    main()
