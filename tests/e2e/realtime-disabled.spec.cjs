const {test,expect}=require('@playwright/test');
test('realtime disabled renders data without opening a socket',async({page,request})=>{
 const sockets=[],errors=[];
 page.on('websocket',socket=>sockets.push(socket));
 page.on('pageerror',error=>errors.push(error.message));
 await page.goto('/');
 await expect(page.locator('body')).toHaveAttribute('data-realtime','false');
 await expect(page.locator('#workflow-repo-filter option')).toHaveCount(4);
 await expect(page.locator('#workflows-container tbody tr')).not.toHaveCount(0);
 await page.evaluate(()=>connectWebSocket());
 expect(sockets).toEqual([]);
 expect((await request.get('/ws')).status()).toBe(404);
 expect(errors).toEqual([]);
});
