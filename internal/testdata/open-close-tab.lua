t1 = tab.new()
t1:go(TEST.url()):wait("body")
assert.eq(t1("body").text, "hello world!")
assert.eq(t1.title, "world - test")
t1:close()

t2 = tab.new(TEST.url("/?target=webscenario")):wait("body")
assert.eq(t2("body").text, "hello webscenario!")
assert.eq(t2.title, "webscenario - test")
t2:close()
