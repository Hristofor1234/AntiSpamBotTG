from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext, CommandHandler
from telegram.error import BadRequest

# Список ключевых слов для фильтрации спама
SPAM_KEYWORDS = ["спам", "реклама", "порно", "секс", "заработок"]

async def handle_message(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()
    if any(keyword in message_text for keyword in SPAM_KEYWORDS):
        try:
            await update.message.delete()
            await context.bot.send_message(
                chat_id=update.message.chat_id,
                text=f"Сообщение от {update.message.from_user.full_name} было удалено как спам."
            )
        except Exception as e:
            print(f"Ошибка при удалении сообщения: {e}")

# Команда для удаления существующего спама
async def clean_spam(update: Update, context: CallbackContext):
    chat_id = update.effective_chat.id
    try:
        # Перебор всех сообщений в канале
        async for message in context.bot.get_chat_history(chat_id):
            if message.text and any(keyword in message.text.lower() for keyword in SPAM_KEYWORDS):
                try:
                    await message.delete()
                except BadRequest:
                    pass  # Игнорируем сообщения, которые нельзя удалить
        await update.message.reply_text("Очистка завершена. Спам удалён.")
    except Exception as e:
        print(f"Ошибка при очистке чата: {e}")
        await update.message.reply_text("Произошла ошибка при очистке чата.")

# Команда для добавления новых ключевых слов
async def add_keyword(update: Update, context: CallbackContext):
    new_keywords = context.args
    if new_keywords:
        SPAM_KEYWORDS.extend(new_keywords)
        await update.message.reply_text(f"Ключевые слова добавлены: {', '.join(new_keywords)}")
    else:
        await update.message.reply_text("Укажите ключевые слова после команды.")

# Команда для просмотра текущих ключевых слов
async def list_keywords(update: Update, context: CallbackContext):
    await update.message.reply_text(f"Текущие ключевые слова: {', '.join(SPAM_KEYWORDS)}")

# Основная функция запуска бота
def main():
    application = Application.builder().token("7706488866:AAH5rPfgUA0zDY_D3wqbcHc7DAxAfLgxQDE").build()

    # Обработчики сообщений и команд
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))
    application.add_handler(CommandHandler("clean", clean_spam))
    application.add_handler(CommandHandler("add", add_keyword))
    application.add_handler(CommandHandler("list", list_keywords))

    application.run_polling()

if __name__ == '__main__':
    main()
