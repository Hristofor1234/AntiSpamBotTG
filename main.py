from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Список ключевых слов для фильтрации спама
SPAM_KEYWORDS = ["спам", "реклама", "порно", "секс", "заработок"]

# Список разрешённых пользователей (username)
ALLOWED_USERS = ["khristo_01"]  # Замените на реальные usernames

def is_user_allowed(username):
    """Проверяет, есть ли пользователь в списке разрешённых."""
    return username in ALLOWED_USERS

# Проверка доступа к командам
def check_access(func):
    """Декоратор для проверки доступа к команде."""
    async def wrapper(update: Update, context: CallbackContext):
        username = update.message.from_user.username
        if not username or not is_user_allowed(username):
            await update.message.reply_text("У вас нет прав на выполнение этой команды.")
            return
        await func(update, context)
    return wrapper

@check_access
async def add_keyword(update: Update, context: CallbackContext):
    """Добавляет новые ключевые слова в список фильтрации."""
    new_keywords = [kw.lower() for kw in context.args]
    added_keywords = []

    for keyword in new_keywords:
        if keyword not in SPAM_KEYWORDS:
            SPAM_KEYWORDS.append(keyword)
            added_keywords.append(keyword)

    if added_keywords:
        await update.message.reply_text(f"Ключевые слова добавлены: {', '.join(added_keywords)}")
    else:
        await update.message.reply_text("Все указанные ключевые слова уже существуют.")

@check_access
async def remove_keyword(update: Update, context: CallbackContext):
    """Удаляет ключевые слова из списка фильтрации."""
    remove_keywords = [kw.lower() for kw in context.args]
    removed_keywords = []

    for keyword in remove_keywords:
        if keyword in SPAM_KEYWORDS:
            SPAM_KEYWORDS.remove(keyword)
            removed_keywords.append(keyword)

    if removed_keywords:
        await update.message.reply_text(f"Ключевые слова удалены: {', '.join(removed_keywords)}")
    else:
        await update.message.reply_text("Указанные ключевые слова не найдены в списке.")

@check_access
async def list_keywords(update: Update, context: CallbackContext):
    """Показывает список текущих ключевых слов."""
    await update.message.reply_text(f"Текущие ключевые слова: {', '.join(SPAM_KEYWORDS)}")

@check_access
async def list_commands(update: Update, context: CallbackContext):
    """Показывает список доступных команд пользователю."""
    await update.message.reply_text(
        "Доступные команды:\n"
        "/add <слово> - Добавить ключевое слово\n"
        "/remove <слово> - Удалить ключевое слово\n"
        "/list - Показать список ключевых слов"
    )

def main():
    application = Application.builder().token("7706488866:AAH5rPfgUA0zDY_D3wqbcHc7DAxAfLgxQDE").build()

    # Обработчики сообщений и команд
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))
    application.add_handler(CommandHandler("commands", list_commands))  # Команда для списка доступных команд

    application.run_polling()

if __name__ == '__main__':
    main()
