from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest
import logging

# Логирование
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Список ключевых слов для фильтрации спама
SPAM_KEYWORDS = ["спам", "реклама", "порно", "секс", "заработок"]

async def handle_message(update: Update, context: CallbackContext):
    """Обрабатывает входящие сообщения и удаляет спам."""
    message_text = update.message.text.lower()
    if any(keyword in message_text for keyword in SPAM_KEYWORDS):
        try:
            await update.message.delete()
            logger.info(f"Удалено сообщение от {update.message.from_user.full_name}: {update.message.text}")
        except BadRequest as e:
            logger.error(f"Не удалось удалить сообщение: {e}")
        except Exception as e:
            logger.error(f"Ошибка при обработке сообщения: {e}")

# Команда для добавления новых ключевых слов
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

# Команда для удаления ключевых слов
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

# Команда для просмотра текущих ключевых слов
async def list_keywords(update: Update, context: CallbackContext):
    """Показывает список текущих ключевых слов."""
    await update.message.reply_text(f"Текущие ключевые слова: {', '.join(SPAM_KEYWORDS)}")

# Основная функция запуска бота
def main():
    application = Application.builder().token("7706488866:AAH5rPfgUA0zDY_D3wqbcHc7DAxAfLgxQDE").build()

    # Обработчики сообщений и команд
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("remove", remove_keyword))
    application.add_handler(CommandHandler("list", list_keywords))

    application.run_polling()

if __name__ == '__main__':
    main()
