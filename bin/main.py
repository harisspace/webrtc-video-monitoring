import RPi.GPIO as GPIO
import sys
from time import sleep

servoPin = 12
GPIO.setmode(GPIO.BOARD)
GPIO.setup(servoPin, GPIO.OUT)

pwm = GPIO.PWM(servoPin, 50)
pwm.start(0)

degree = float(sys.argv[1])

dc = (degree/18) + 2

waitTime = 4*10+70
waitTimeInSecond = waitTime/1000

pwm.ChangeDutyCycle(dc)
sleep(waitTimeInSecond)

pwm.stop()
GPIO.cleanup()
