from telegram import Update
from telegram.ext import Application, MessageHandler, filters, CallbackContext

# Список ключевых слов или шаблонов, по которым будет фильтроваться спам
SPAM_KEYWORDS = ["спам", "реклама", "порно", "секс"]

async def handle_message(update: Update, context: CallbackContext):
    message_text = update.message.text.lower()  # Приведение текста к нижнему регистру
    # Проверка на наличие спам-ключевых слов
    if any(keyword in message_text for keyword in SPAM_KEYWORDS):
        try:
            await update.message.delete()  # Удаление сообщения, если оно содержит спам
            await context.bot.send_message(
                chat_id=update.message.chat_id,
                text=""
            )
        except Exception as e:
            print(f"Ошибка при удалении сообщения: {e}")

def main():
    # Замените "YOUR_TELEGRAM_BOT_TOKEN" на токен вашего бота
    application = Application.builder().token("7706488866:AAH5rPfgUA0zDY_D3wqbcHc7DAxAfLgxQDE").build()

    # Добавление обработчика для текстовых сообщений
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    application.run_polling()

if __name__ == '__main__':
    main()


