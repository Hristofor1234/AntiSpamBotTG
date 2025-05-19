from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging
import json
import os
from dotenv import load_dotenv

# Загрузка переменных окружения из файла .env
load_dotenv()

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
    with open(KEYWORDS_FILE, "w", encoding="utf-8") as file:
        json.dump(SPAM_KEYWORDS, file, ensure_ascii=False, indent=4)


def load_keywords():
    global SPAM_KEYWORDS
    try:
        with open(KEYWORDS_FILE, "r", encoding="utf-8") as file:
            SPAM_KEYWORDS = json.load(file)
        logger.info(f"Загружены ключевые слова: {SPAM_KEYWORDS}")
    except FileNotFoundError:
        SPAM_KEYWORDS = []
        logger.warning("Файл keywords.json не найден. Начинаем с пустого списка.")


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

# Команды для управления ключевыми словами
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

# Удаление сообщений с ключевыми словами
async def filter_spam(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()
    for keyword in SPAM_KEYWORDS:
        if keyword in message_text:
            try:
                await update.message.delete()
                logger.info(f"Сообщение удалено: {message_text}")
                return
            except BadRequest as e:
                logger.error(f"Ошибка при удалении сообщения: {e}")
                return

def main():
    # Загрузка ключевых слов
    load_keywords()

    # Получение токена из переменной окружения
    token = os.getenv("TELEGRAM_BOT_TOKEN")
    if not token:
        logger.error("Токен не найден в переменных окружения!")
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
